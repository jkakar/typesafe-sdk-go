package typesafe_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/jkakar/typesafe-sdk-go"
)

// sampleAnswers holds one answer of each modeled type.
func sampleAnswers() typesafe.Answers {
	return typesafe.Answers{
		"urgent": typesafe.NoulAnswer{Noul: 0.92},
		"department": typesafe.ChoiceAnswer{
			Choice:        "technical",
			Confidence:    0.82,
			Probabilities: map[string]float64{"billing": 0.15, "technical": 0.85},
		},
		"frustration": typesafe.ScoreAnswer{
			Score:         1.6,
			Confidence:    0.78,
			Legend:        map[int]any{0: "Calm", 1: "Frustrated"},
			Probabilities: map[int]float64{0: 0.35, 1: 0.65},
		},
	}
}

func TestAnswers_Noul(t *testing.T) {
	t.Parallel()

	t.Run("returns the answer for a yes/no question", func(t *testing.T) {
		t.Parallel()

		answer, err := sampleAnswers().Noul("urgent")

		assert.NoError(t, err)
		assert.Equal(t, 0.92, answer.Noul)
	})

	t.Run("reports an unanswered question", func(t *testing.T) {
		t.Parallel()

		_, err := sampleAnswers().Noul("missing")

		assert.IsError(t, err, typesafe.ErrNoAnswer)
		assert.Contains(t, err.Error(), `"missing"`)
	})

	t.Run("reports an answer of another type", func(t *testing.T) {
		t.Parallel()

		_, err := sampleAnswers().Noul("department")

		var typeErr *typesafe.AnswerTypeError
		assert.True(t, errors.As(err, &typeErr))
		assert.Equal(t, typesafe.AnswerTypeError{Name: "department", Want: "noul", Got: "choice"}, *typeErr)
	})
}

func TestAnswers_Choice(t *testing.T) {
	t.Parallel()

	t.Run("returns the answer for a choice question", func(t *testing.T) {
		t.Parallel()

		answer, err := sampleAnswers().Choice("department")

		assert.NoError(t, err)
		assert.Equal(t, "technical", answer.Choice)
		assert.Equal(t, 0.82, answer.Confidence)
	})

	t.Run("reports an unanswered question", func(t *testing.T) {
		t.Parallel()

		_, err := sampleAnswers().Choice("missing")

		assert.IsError(t, err, typesafe.ErrNoAnswer)
	})

	t.Run("reports an answer of another type", func(t *testing.T) {
		t.Parallel()

		_, err := sampleAnswers().Choice("frustration")

		var typeErr *typesafe.AnswerTypeError
		assert.True(t, errors.As(err, &typeErr))
		assert.Equal(t, "score", typeErr.Got)
	})
}

func TestAnswers_Score(t *testing.T) {
	t.Parallel()

	t.Run("returns the answer for a score question", func(t *testing.T) {
		t.Parallel()

		answer, err := sampleAnswers().Score("frustration")

		assert.NoError(t, err)
		assert.Equal(t, 1.6, answer.Score)
		assert.Equal(t, "Frustrated", answer.Legend[1])
	})

	t.Run("reports an unanswered question", func(t *testing.T) {
		t.Parallel()

		_, err := sampleAnswers().Score("missing")

		assert.IsError(t, err, typesafe.ErrNoAnswer)
	})

	t.Run("reports an answer of another type", func(t *testing.T) {
		t.Parallel()

		_, err := sampleAnswers().Score("urgent")

		var typeErr *typesafe.AnswerTypeError
		assert.True(t, errors.As(err, &typeErr))
		assert.Equal(t, "noul", typeErr.Got)
	})
}

func TestAnswers_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("decodes each answer into the type its discriminator names", func(t *testing.T) {
		t.Parallel()
		body := `{
			"urgent": {"type": "noul", "noul": 0.92},
			"department": {"type": "choice", "choice": "technical", "confidence": 0.82,
				"probabilities": {"billing": 0.15, "technical": 0.85}},
			"frustration": {"type": "score", "score": 1.6, "confidence": 0.78,
				"legend": {"0": "Calm", "1": "Frustrated"},
				"probabilities": {"0": 0.35, "1": 0.65}}
		}`

		var answers typesafe.Answers
		err := json.Unmarshal([]byte(body), &answers)

		assert.NoError(t, err)
		assert.Equal(t, sampleAnswers(), answers)
	})

	t.Run("keeps an answer type it does not model", func(t *testing.T) {
		t.Parallel()

		var answers typesafe.Answers
		err := json.Unmarshal([]byte(`{"mood":{"type":"vector","values":[1]}}`), &answers)

		assert.NoError(t, err)
		assert.Equal[typesafe.Answer](t, typesafe.UnknownAnswer{
			Type: "vector",
			Raw:  json.RawMessage(`{"type":"vector","values":[1]}`),
		}, answers["mood"])
	})

	t.Run("reports a malformed answer map", func(t *testing.T) {
		t.Parallel()

		var answers typesafe.Answers
		err := json.Unmarshal([]byte(`[]`), &answers)

		assert.Error(t, err)
	})

	t.Run("reports a malformed discriminator", func(t *testing.T) {
		t.Parallel()

		var answers typesafe.Answers
		err := json.Unmarshal([]byte(`{"urgent":{"type":7}}`), &answers)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "urgent")
	})

	t.Run("reports a malformed answer", func(t *testing.T) {
		t.Parallel()

		var answers typesafe.Answers
		err := json.Unmarshal([]byte(`{"urgent":{"type":"noul","noul":"high"}}`), &answers)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "urgent")
	})
}

func TestNoulAnswer_MarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("encodes the answer with its discriminator", func(t *testing.T) {
		t.Parallel()

		body, err := json.Marshal(typesafe.NoulAnswer{Noul: 0.92})

		assert.NoError(t, err)
		assert.Equal(t, `{"type":"noul","noul":0.92}`, string(body))
	})
}

func TestChoiceAnswer_MarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("encodes the answer with its discriminator", func(t *testing.T) {
		t.Parallel()

		body, err := json.Marshal(typesafe.ChoiceAnswer{
			Choice:        "technical",
			Confidence:    0.82,
			Probabilities: map[string]float64{"technical": 0.85},
		})

		assert.NoError(t, err)
		assert.Equal(t, `{"type":"choice","choice":"technical","confidence":0.82,`+
			`"probabilities":{"technical":0.85}}`, string(body))
	})
}

func TestScoreAnswer_MarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("encodes the answer with its discriminator", func(t *testing.T) {
		t.Parallel()

		body, err := json.Marshal(typesafe.ScoreAnswer{
			Score:         1.6,
			Confidence:    0.78,
			Legend:        map[int]any{0: "Calm"},
			Probabilities: map[int]float64{0: 1},
		})

		assert.NoError(t, err)
		assert.Equal(t, `{"type":"score","score":1.6,"confidence":0.78,`+
			`"legend":{"0":"Calm"},"probabilities":{"0":1}}`, string(body))
	})

	t.Run("reports a legend that cannot be encoded", func(t *testing.T) {
		t.Parallel()

		_, err := json.Marshal(typesafe.ScoreAnswer{Legend: map[int]any{0: make(chan int)}})

		assert.Error(t, err)
	})
}

func TestUnknownAnswer_MarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("returns the answer's json unchanged", func(t *testing.T) {
		t.Parallel()
		answer := typesafe.UnknownAnswer{Type: "vector", Raw: json.RawMessage(`{"type":"vector"}`)}

		body, err := json.Marshal(answer)

		assert.NoError(t, err)
		assert.Equal(t, `{"type":"vector"}`, string(body))
	})
}

func TestNoulAnswer_Kind(t *testing.T) {
	t.Parallel()

	t.Run("names the question type it answers", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, typesafe.KindNoul, typesafe.NoulAnswer{}.Kind())
	})
}

func TestChoiceAnswer_Kind(t *testing.T) {
	t.Parallel()

	t.Run("names the question type it answers", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, typesafe.KindChoice, typesafe.ChoiceAnswer{}.Kind())
	})
}

func TestScoreAnswer_Kind(t *testing.T) {
	t.Parallel()

	t.Run("names the question type it answers", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, typesafe.KindScore, typesafe.ScoreAnswer{}.Kind())
	})
}

func TestUnknownAnswer_Kind(t *testing.T) {
	t.Parallel()

	t.Run("returns the type the api sent", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "vector", typesafe.UnknownAnswer{Type: "vector"}.Kind())
	})
}

func TestAnswerTypeError_Error(t *testing.T) {
	t.Parallel()

	t.Run("names the question and both types", func(t *testing.T) {
		t.Parallel()
		err := &typesafe.AnswerTypeError{Name: "department", Want: "noul", Got: "choice"}

		assert.Equal(t, `answer "department" is a choice answer, not a noul answer`, err.Error())
	})
}
