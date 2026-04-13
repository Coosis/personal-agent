package memorysuggestions

type mergePolicy string

const (
	mergePolicyAppend  mergePolicy = "append"
	mergePolicyReplace mergePolicy = "replace"
)

var canonicalKeyPolicies = map[string]mergePolicy{
	"profile.university":        mergePolicyReplace,
	"profile.field_of_study":    mergePolicyReplace,
	"profile.current_city":      mergePolicyReplace,
	"project.status":            mergePolicyReplace,
	"event.notable_event":       mergePolicyAppend,
	"project.past_project":      mergePolicyAppend,
	"relationship.known_person": mergePolicyAppend,
	"preference.liked_language": mergePolicyAppend,
}

func policyFor(category, key string) mergePolicy {
	if policy, ok := canonicalKeyPolicies[category+"."+key]; ok {
		return policy
	}
	return mergePolicyAppend
}
