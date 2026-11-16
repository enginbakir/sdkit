package workflow

import "testing"

func TestPolicyTable(t *testing.T) {
	for _, c := range []struct{ risk, profile, ver, dev string }{
		{"low", "standard", "verify-command", "off"}, {"medium", "standard", "independent-verifier", "off"},
		{"high", "high-assurance", "independent-verifier", "required"}, {"low", "high-assurance", "independent-verifier", "required"},
		{"low", "small", "verify-command", "off"}, {"medium", "small", "", ""}, {"low", "bogus", "", ""},
	} {
		err := Check(c.profile, c.risk)
		if (err != nil) != (c.ver == "") {
			t.Errorf("%v: Check = %v", c, err)
		} else if err == nil && (Verifier(c.risk, c.profile) != c.ver || Contracts(c.profile, false, "developer") != c.dev) {
			t.Errorf("%v: got %s %s", c, Verifier(c.risk, c.profile), Contracts(c.profile, false, "developer"))
		}
	}
	if len(EvidenceRoles("high")) != 3 || len(EvidenceRoles("low")) != 0 || Contracts("high-assurance", false, "x:scout") != "optional" {
		t.Error("evidence roles or lookup level")
	}
}
