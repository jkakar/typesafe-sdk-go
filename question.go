package typesafe

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Question kinds, as they appear in the type field of a question and of the
// answer it produces.
const (
	KindNoul   = "noul"
	KindChoice = "choice"
	KindScore  = "score"
)

// Errors reported for a request the API would reject.
var (
	// ErrNoQuestions reports a request that asks nothing.
	ErrNoQuestions = errors.New("no questions")
	// ErrNoCriteria reports a choice or score question with empty criteria.
	ErrNoCriteria = errors.New("no criteria")
	// ErrInvalidState reports state that does not encode as a JSON string,
	// object, or array.
	ErrInvalidState = errors.New("invalid state")
)

// Questions are the questions in a request, keyed by the names their answers
// come back under. A name is chosen by the caller and is not sent to the
// model.
type Questions map[string]Question

// A Question is one of [NoulQuestion], [ChoiceQuestion], [ScoreQuestion], or
// [RawQuestion]. Only this package implements it, so a type switch over the
// concrete questions stays exhaustive.
type Question interface {
	// validate reports criteria the API rejects. It also seals the
	// interface, because only this package can implement it.
	validate() error
}

// Instructions, criteria, and state accept text, a JSON object, or a JSON
// array. Any value that encodes to one of those shapes is accepted, including
// a string, a map, a slice, or a struct with JSON tags. A nil value leaves the
// field out of the request.

// NoulQuestion asks a yes/no question. Its answer is the probability that the
// answer is yes. See https://docs.typesafe.ai/primitives/noul.
type NoulQuestion struct {
	// Instructions is the yes/no question to evaluate.
	Instructions any `json:"instructions,omitempty"`
	// Criteria optionally describes what a yes and a no mean.
	Criteria *NoulCriteria `json:"criteria,omitempty"`
}

// NoulCriteria describes the two outcomes of a [NoulQuestion]. A nil field
// leaves that outcome undescribed.
type NoulCriteria struct {
	// True describes what a probability near one means.
	True any `json:"true,omitempty"`
	// False describes what a probability near zero means.
	False any `json:"false,omitempty"`
}

func (q NoulQuestion) validate() error { return nil }

// MarshalJSON encodes the question with its type discriminator.
func (q NoulQuestion) MarshalJSON() ([]byte, error) {
	type wire NoulQuestion
	return marshalTagged(KindNoul, wire(q))
}

// ChoiceQuestion selects one option from a set. Its answer names the most
// probable option and reports a probability for every option. See
// https://docs.typesafe.ai/primitives/choice.
type ChoiceQuestion struct {
	// Instructions is what the model should decide.
	Instructions any `json:"instructions,omitempty"`
	// Criteria names each option and describes when it applies. It must
	// hold at least one option.
	Criteria ChoiceCriteria `json:"criteria"`
}

// ChoiceCriteria maps each option of a [ChoiceQuestion] to a description of
// when it applies. A nil description leaves the option to be read from its
// name alone.
type ChoiceCriteria map[string]any

func (q ChoiceQuestion) validate() error {
	if len(q.Criteria) == 0 {
		return fmt.Errorf("%w: a choice question needs at least one option", ErrNoCriteria)
	}
	return nil
}

// MarshalJSON encodes the question with its type discriminator.
func (q ChoiceQuestion) MarshalJSON() ([]byte, error) {
	type wire ChoiceQuestion
	return marshalTagged(KindChoice, wire(q))
}

// ScoreQuestion rates the state against an ordered rubric. Its answer is the
// probability-weighted average of the levels, which may fall between them. See
// https://docs.typesafe.ai/primitives/score.
type ScoreQuestion struct {
	// Instructions is what the model should rate.
	Instructions any `json:"instructions,omitempty"`
	// Criteria describes each level in ascending order, starting at score
	// zero. It must hold at least one level, and the API documentation
	// recommends at least two.
	Criteria ScoreCriteria `json:"criteria"`
}

// ScoreCriteria describes the levels of a [ScoreQuestion] in ascending order.
// A description's position is its score, counting from zero.
type ScoreCriteria []any

func (q ScoreQuestion) validate() error {
	if len(q.Criteria) == 0 {
		return fmt.Errorf("%w: a score question needs at least one level", ErrNoCriteria)
	}
	return nil
}

// MarshalJSON encodes the question with its type discriminator.
func (q ScoreQuestion) MarshalJSON() ([]byte, error) {
	type wire ScoreQuestion
	return marshalTagged(KindScore, wire(q))
}

// RawQuestion carries a question encoded as JSON, for a question type this
// SDK version does not model. It is sent exactly as given, so a program can
// use a new question type before this package names it.
type RawQuestion struct {
	// Type is the question's type field.
	Type string
	// JSON is the complete question object, including its type field.
	JSON json.RawMessage
}

func (q RawQuestion) validate() error {
	if q.Type == "" {
		return errors.New("a raw question needs a type")
	}
	if len(q.JSON) == 0 {
		return errors.New("a raw question needs a JSON body")
	}
	return nil
}

// MarshalJSON returns the question's JSON unchanged.
func (q RawQuestion) MarshalJSON() ([]byte, error) { return q.JSON, nil }

// validate reports the first question the API would reject.
func (q Questions) validate() error {
	if len(q) == 0 {
		return ErrNoQuestions
	}
	for _, name := range sortedKeys(q) {
		if err := q[name].validate(); err != nil {
			return fmt.Errorf("question %q: %w", name, err)
		}
	}
	return nil
}

// UnmarshalJSON decodes each question into the type its discriminator names,
// keeping an unrecognized type in a [RawQuestion].
func (q *Questions) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	questions := make(Questions, len(raw))
	for _, name := range sortedKeys(raw) {
		question, err := decodeQuestion(raw[name])
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		questions[name] = question
	}
	*q = questions
	return nil
}

// decodeQuestion decodes one question, keeping an unrecognized type intact.
func decodeQuestion(raw json.RawMessage) (Question, error) {
	kind, err := kindOf(raw)
	if err != nil {
		return nil, err
	}
	switch kind {
	case KindNoul:
		return unmarshalTagged[NoulQuestion](raw)
	case KindChoice:
		return unmarshalTagged[ChoiceQuestion](raw)
	case KindScore:
		return unmarshalTagged[ScoreQuestion](raw)
	default:
		return RawQuestion{Type: kind, JSON: slices.Clone(raw)}, nil
	}
}
