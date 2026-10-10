package session

import "fmt"

// The enums are written to the event log by name, not number, so the log
// reads well and survives reordering the constants.

func (k QuestionKind) MarshalText() ([]byte, error) { return []byte(k.FixtureName()), nil }

func (k *QuestionKind) UnmarshalText(b []byte) error {
	v, ok := KindFromFixture(string(b))
	if !ok {
		return fmt.Errorf("unknown Question Kind %q", b)
	}
	*k = v
	return nil
}

var anchorTypeNames = []string{"diff", "file", "description", "briefing", "answer", "pr", "comment"}

func (a AnchorType) MarshalText() ([]byte, error) {
	if int(a) < 0 || int(a) >= len(anchorTypeNames) {
		return nil, fmt.Errorf("bad AnchorType %d", a)
	}
	return []byte(anchorTypeNames[a]), nil
}

func (a *AnchorType) UnmarshalText(b []byte) error {
	for i, n := range anchorTypeNames {
		if n == string(b) {
			*a = AnchorType(i)
			return nil
		}
	}
	return fmt.Errorf("unknown Anchor type %q", b)
}

var feedbackStatusNames = []string{"draft", "ready", "queued", "handed_off", "posted", "done"}

func (s FeedbackStatus) MarshalText() ([]byte, error) {
	if int(s) < 0 || int(s) >= len(feedbackStatusNames) {
		return nil, fmt.Errorf("bad FeedbackStatus %d", s)
	}
	return []byte(feedbackStatusNames[s]), nil
}

func (s *FeedbackStatus) UnmarshalText(b []byte) error {
	for i, n := range feedbackStatusNames {
		if n == string(b) {
			*s = FeedbackStatus(i)
			return nil
		}
	}
	return fmt.Errorf("unknown Feedback status %q", b)
}

var categoryNames = []string{"endpoint", "schema", "config", "signature"}

func (c ContractCategory) MarshalText() ([]byte, error) {
	if int(c) < 0 || int(c) >= len(categoryNames) {
		return nil, fmt.Errorf("bad ContractCategory %d", c)
	}
	return []byte(categoryNames[c]), nil
}

func (c *ContractCategory) UnmarshalText(b []byte) error {
	for i, n := range categoryNames {
		if n == string(b) {
			*c = ContractCategory(i)
			return nil
		}
	}
	return fmt.Errorf("unknown Contract Change category %q", b)
}

// CategoryFromName maps the agent's category names (the union schema's
// contract_changes.type) to a ContractCategory; unknown names are Config.
func CategoryFromName(s string) ContractCategory {
	var c ContractCategory
	if c.UnmarshalText([]byte(s)) != nil {
		return CategoryConfig
	}
	return c
}
