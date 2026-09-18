package typesafe

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// ErrNoAnswer reports a question name the response does not answer.
var ErrNoAnswer = errors.New("no answer")

// Answers holds every answer in a response, keyed by the question names the
// request used.
type Answers map[string]Answer

// An Answer is one of [NoulAnswer], [ChoiceAnswer], [ScoreAnswer], or
// [UnknownAnswer]. Read a known answer with [Answers.Noul], [Answers.Choice],
// or [Answers.Score], or switch over the concrete types.
type Answer interface {
	// Kind returns the answer's type field, one of [KindNoul],
	// [KindChoice], [KindScore], or, for an [UnknownAnswer], whatever the
	// API sent.
	Kind() string

	// seal keeps implementations inside this package, so a type switch
	// over the concrete answers stays exhaustive.
	seal()
}

// NoulAnswer answers a [NoulQuestion].
type NoulAnswer struct {
	// Noul is the probability of a yes answer, from zero to one. Values
	// near one favor yes, values near zero favor no, and values near a
	// half report uncertainty.
	Noul float64 `json:"noul"`
}

// Kind returns [KindNoul].
func (a NoulAnswer) Kind() string { return KindNoul }
func (a NoulAnswer) seal()        {}

// ChoiceAnswer answers a [ChoiceQuestion].
type ChoiceAnswer struct {
	// Choice is the most probable option.
	Choice string `json:"choice"`
	// Confidence reports how certain the model is, from zero to one.
	Confidence float64 `json:"confidence"`
	// Probabilities holds the probability of every option, keyed by option
	// name. The values sum to approximately one.
	Probabilities map[string]float64 `json:"probabilities"`
}

// Kind returns [KindChoice].
func (a ChoiceAnswer) Kind() string { return KindChoice }
func (a ChoiceAnswer) seal()        {}

// ScoreAnswer answers a [ScoreQuestion].
type ScoreAnswer struct {
	// Score is the probability-weighted average of the levels, which may
	// fall between them.
	Score float64 `json:"score"`
	// Confidence reports how certain the model is, from zero to one.
	Confidence float64 `json:"confidence"`
	// Legend maps each level to the description the request supplied.
	Legend map[int]any `json:"legend"`
	// Probabilities holds the probability of every level, keyed by level.
	// The values sum to approximately one.
	Probabilities map[int]float64 `json:"probabilities"`
}

// Kind returns [KindScore].
func (a ScoreAnswer) Kind() string { return KindScore }
func (a ScoreAnswer) seal()        {}

// UnknownAnswer carries an answer whose type this SDK version does not model,
// so a newer API does not break an older client.
type UnknownAnswer struct {
	// Type is the type field the API sent.
	Type string
	// Raw is the answer's JSON, exactly as it arrived.
	Raw json.RawMessage
}

// Kind returns the type field the API sent.
func (a UnknownAnswer) Kind() string { return a.Type }
func (a UnknownAnswer) seal()        {}

// An AnswerTypeError reports an answer that has a different type than the
// caller asked for.
type AnswerTypeError struct {
	// Name is the question name the caller read.
	Name string
	// Want is the type the caller asked for.
	Want string
	// Got is the type the API returned.
	Got string
}

func (e *AnswerTypeError) Error() string {
	return fmt.Sprintf("answer %q is a %s answer, not a %s answer", e.Name, e.Got, e.Want)
}

// Noul returns the [NoulAnswer] for name.
func (a Answers) Noul(name string) (NoulAnswer, error) {
	return readAnswer[NoulAnswer](a, name, KindNoul)
}

// Choice returns the [ChoiceAnswer] for name.
func (a Answers) Choice(name string) (ChoiceAnswer, error) {
	return readAnswer[ChoiceAnswer](a, name, KindChoice)
}

// Score returns the [ScoreAnswer] for name.
func (a Answers) Score(name string) (ScoreAnswer, error) {
	return readAnswer[ScoreAnswer](a, name, KindScore)
}

// readAnswer returns the answer for name when it has the requested type. It
// reports [ErrNoAnswer] when name is absent and an [AnswerTypeError] when the
// answer has another type.
func readAnswer[T Answer](answers Answers, name, want string) (T, error) {
	var zero T
	answer, ok := answers[name]
	if !ok {
		return zero, fmt.Errorf("%w for question %q", ErrNoAnswer, name)
	}
	typed, ok := answer.(T)
	if !ok {
		return zero, &AnswerTypeError{Name: name, Want: want, Got: answer.Kind()}
	}
	return typed, nil
}

// UnmarshalJSON decodes each answer into the type its discriminator names.
func (a *Answers) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	answers := make(Answers, len(raw))
	for _, name := range sortedKeys(raw) {
		answer, err := decodeAnswer(raw[name])
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		answers[name] = answer
	}
	*a = answers
	return nil
}

// decodeAnswer decodes one answer, keeping an unrecognized type intact.
func decodeAnswer(raw json.RawMessage) (Answer, error) {
	kind, err := kindOf(raw)
	if err != nil {
		return nil, err
	}
	switch kind {
	case KindNoul:
		return unmarshalTagged[NoulAnswer](raw)
	case KindChoice:
		return unmarshalTagged[ChoiceAnswer](raw)
	case KindScore:
		return unmarshalTagged[ScoreAnswer](raw)
	default:
		return UnknownAnswer{Type: kind, Raw: slices.Clone(raw)}, nil
	}
}

// MarshalJSON encodes the answer with its type discriminator.
func (a NoulAnswer) MarshalJSON() ([]byte, error) {
	type wire NoulAnswer
	return marshalTagged(KindNoul, wire(a))
}

// MarshalJSON encodes the answer with its type discriminator.
func (a ChoiceAnswer) MarshalJSON() ([]byte, error) {
	type wire ChoiceAnswer
	return marshalTagged(KindChoice, wire(a))
}

// MarshalJSON encodes the answer with its type discriminator.
func (a ScoreAnswer) MarshalJSON() ([]byte, error) {
	type wire ScoreAnswer
	return marshalTagged(KindScore, wire(a))
}

// MarshalJSON returns the answer's JSON unchanged.
func (a UnknownAnswer) MarshalJSON() ([]byte, error) { return a.Raw, nil }
