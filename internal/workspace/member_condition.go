package workspace

import "strings"

// MemberCondition is a strong item validator, never an identity or grant.
func MemberCondition(id, role string) string { return `"membership:` + id + `:` + role + `"` }
func parseMemberCondition(s string) (string, string, bool) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", "", false
	}
	p := strings.Split(s[1:len(s)-1], ":")
	if len(p) != 3 || p[0] != "membership" || !validUUID(p[1]) || (p[2] != "member" && p[2] != "owner") {
		return "", "", false
	}
	return p[1], p[2], true
}
func validMemberCondition(s string) bool { _, _, ok := parseMemberCondition(s); return ok }
