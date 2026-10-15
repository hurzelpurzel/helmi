package resources

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"

	"helmi/internal/errs"
	"helmi/internal/yamlutil"

	"gopkg.in/yaml.v3"
)

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Identity is a stable Kubernetes resource identity.
type Identity struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
}

// Text returns a readable identity string.
func (id Identity) Text() string {
	scope := ""
	if id.Namespace != "" {
		scope = id.Namespace + "/"
	}
	return id.APIVersion + ":" + id.Kind + ":" + scope + id.Name
}

// Less returns true if id is less than other in canonical order.
func (id Identity) Less(other Identity) bool {
	if id.APIVersion != other.APIVersion {
		return id.APIVersion < other.APIVersion
	}
	if id.Kind != other.Kind {
		return id.Kind < other.Kind
	}
	if id.Namespace != other.Namespace {
		return id.Namespace < other.Namespace
	}
	return id.Name < other.Name
}

// IdentitySlice provides sorting.
type IdentitySlice []Identity

func (s IdentitySlice) Len() int           { return len(s) }
func (s IdentitySlice) Less(i, j int) bool { return s[i].Less(s[j]) }
func (s IdentitySlice) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// ResourceIdentity extracts resource identity.
func ResourceIdentity(node *yaml.Node) (Identity, error) {
	if !yamlutil.IsMapping(node) {
		return Identity{}, errs.Configurationf("Rendered resource is missing a mapping")
	}
	apiVersionNode := yamlutil.MapGet(node, "apiVersion")
	apiVersion := yamlutil.NodeString(apiVersionNode)
	if strings.TrimSpace(apiVersion) == "" {
		return Identity{}, errs.Configurationf("Rendered resource is missing a non-empty apiVersion")
	}
	kindNode := yamlutil.MapGet(node, "kind")
	kind := yamlutil.NodeString(kindNode)
	if strings.TrimSpace(kind) == "" {
		return Identity{}, errs.Configurationf("Rendered resource is missing a non-empty kind")
	}
	metadata := yamlutil.MapGet(node, "metadata")
	if !yamlutil.IsMapping(metadata) {
		return Identity{}, errs.Configurationf("Rendered %s resource is missing metadata", kind)
	}
	name := yamlutil.NodeString(yamlutil.MapGet(metadata, "name"))
	if strings.TrimSpace(name) == "" {
		generateName := yamlutil.NodeString(yamlutil.MapGet(metadata, "generateName"))
		if strings.TrimSpace(generateName) == "" {
			return Identity{}, errs.Configurationf("Rendered %s resource is missing metadata.name", kind)
		}
		name = "generate:" + generateName
	}
	nsNode := yamlutil.MapGet(metadata, "namespace")
	ns := ""
	if nsNode != nil {
		nsVal, ok := yamlutil.ScalarString(nsNode)
		if !ok {
			return Identity{}, errs.Configurationf("Rendered %s/%s has a non-string metadata.namespace", kind, name)
		}
		ns = nsVal
	}
	return Identity{APIVersion: apiVersion, Kind: kind, Namespace: ns, Name: name}, nil
}

// ResourceKey generates Helm values resource key.
func ResourceKey(node *yaml.Node) (string, error) {
	id, err := ResourceIdentity(node)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, 3)
	if id.Kind != "" {
		parts = append(parts, id.Kind)
	}
	if id.Namespace != "" {
		parts = append(parts, id.Namespace)
	}
	if id.Name != "" {
		parts = append(parts, id.Name)
	}
	readable := strings.Join(parts, "-")
	slug := slugRe.ReplaceAllString(strings.ToLower(readable), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "resource"
	}
	if len(slug) > 50 {
		slug = slug[:50]
	}
	slug = strings.TrimRight(slug, "-")
	digest := sha256.Sum256([]byte(id.APIVersion + "\x00" + id.Kind + "\x00" + id.Namespace + "\x00" + id.Name))
	digestHex := hex.EncodeToString(digest[:])
	return slug + "-" + digestHex[:10], nil
}

// Index holds indexed resources.
type Index struct {
	Items map[Identity]*yaml.Node
	Order []Identity
}

// IndexResources indexes resources and validates.
func IndexResources(nodes []*yaml.Node) (*Index, error) {
	items := make(map[Identity]*yaml.Node)
	for i, res := range nodes {
		if !yamlutil.IsMapping(res) {
			return nil, errs.Configurationf("Rendered YAML document %d must be a mapping, got %T", i+1, res)
		}
		id, err := ResourceIdentity(res)
		if err != nil {
			return nil, err
		}
		if _, ok := items[id]; ok {
			return nil, errs.Configurationf("Duplicate rendered resource identity: %s", id.Text())
		}
		items[id] = yamlutil.Clone(res)
	}
	order := make([]Identity, 0, len(items))
	for id := range items {
		order = append(order, id)
	}
	sort.Sort(IdentitySlice(order))
	return &Index{Items: items, Order: order}, nil
}

// AssertResourceEquivalence compares two resource sets.
func AssertResourceEquivalence(expected, actual []*yaml.Node, context string) error {
	expIdx, err := IndexResources(expected)
	if err != nil {
		return err
	}
	actIdx, err := IndexResources(actual)
	if err != nil {
		return err
	}
	expSet := make(map[Identity]struct{}, len(expIdx.Items))
	for id := range expIdx.Items {
		expSet[id] = struct{}{}
	}
	actSet := make(map[Identity]struct{}, len(actIdx.Items))
	for id := range actIdx.Items {
		actSet[id] = struct{}{}
	}
	var missing, unexpected, changed []Identity
	for id := range expSet {
		if _, ok := actSet[id]; !ok {
			missing = append(missing, id)
		}
	}
	for id := range actSet {
		if _, ok := expSet[id]; !ok {
			unexpected = append(unexpected, id)
		}
	}
	for id := range expSet {
		if _, ok := actSet[id]; ok {
			equal, err := yamlutil.DeepEqual(expIdx.Items[id], actIdx.Items[id])
			if err != nil {
				return err
			}
			if !equal {
				changed = append(changed, id)
			}
		}
	}
	sort.Sort(IdentitySlice(missing))
	sort.Sort(IdentitySlice(unexpected))
	sort.Sort(IdentitySlice(changed))
	if len(missing) == 0 && len(unexpected) == 0 && len(changed) == 0 {
		return nil
	}
	var details []string
	if len(missing) > 0 {
		parts := make([]string, 0, len(missing))
		count := 5
		if len(missing) < count {
			count = len(missing)
		}
		for i := 0; i < count; i++ {
			parts = append(parts, missing[i].Text())
		}
		details = append(details, "missing="+strings.Join(parts, ", "))
	}
	if len(unexpected) > 0 {
		parts := make([]string, 0, len(unexpected))
		count := 5
		if len(unexpected) < count {
			count = len(unexpected)
		}
		for i := 0; i < count; i++ {
			parts = append(parts, unexpected[i].Text())
		}
		details = append(details, "unexpected="+strings.Join(parts, ", "))
	}
	if len(changed) > 0 {
		parts := make([]string, 0, len(changed))
		count := 5
		if len(changed) < count {
			count = len(changed)
		}
		for i := 0; i < count; i++ {
			parts = append(parts, changed[i].Text())
		}
		details = append(details, "changed="+strings.Join(parts, ", "))
	}
	return errs.Validationf("Helm render differs from %s: %s", context, strings.Join(details, "; "))
}

// FindHelmBehaviorAnnotations finds helm behavior annotations.
func FindHelmBehaviorAnnotations(nodes []*yaml.Node) ([]string, error) {
	var findings []string
	annSet := map[string]struct{}{"helm.sh/hook": {}, "helm.sh/resource-policy": {}}
	for _, res := range nodes {
		if !yamlutil.IsMapping(res) {
			continue
		}
		id, err := ResourceIdentity(res)
		if err != nil {
			continue
		}
		metadata := yamlutil.MapGet(res, "metadata")
		anns := yamlutil.MapGet(metadata, "annotations")
		if !yamlutil.IsMapping(anns) {
			continue
		}
		var special []string
		for i := 0; i+1 < len(anns.Content); i += 2 {
			k := anns.Content[i]
			key := yamlutil.NodeString(k)
			if _, ok := annSet[key]; ok {
				special = append(special, key)
			}
		}
		if len(special) > 0 {
			sort.Strings(special)
			findings = append(findings, id.Text()+" ("+strings.Join(special, ", ")+")")
		}
	}
	sort.Strings(findings)
	return findings, nil
}
