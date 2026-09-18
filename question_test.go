package typesafe_test

import (
	"encoding/json"
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/jkakar/typesafe-sdk-go"
)

func TestNoulQuestion_MarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("encodes instructions and criteria", func(t *testing.T) {
		t.Parallel()
		question := typesafe.NoulQuestion{
			Instructions: "Does this convey urgency?",
			Criteria:     &typesafe.NoulCriteria{True: "Time sensitive", False: "No urgency"},
		}

		body, err := json.Marshal(question)

		assert.NoError(t, err)
		assert.Equal(t, `{"type":"noul","instructions":"Does this convey urgency?",`+
			`"criteria":{"true":"Time sensitive","false":"No urgency"}}`, string(body))
	})

	t.Run("omits instructions and criteria that are not set", func(t *testing.T) {
		t.Parallel()

		body, err := json.Marshal(typesafe.NoulQuestion{})

		assert.NoError(t, err)
		assert.Equal(t, `{"type":"noul"}`, string(body))
	})

	t.Run("encodes structured instructions", func(t *testing.T) {
		t.Parallel()
		question := typesafe.NoulQuestion{Instructions: map[string]any{"task": "Find spam"}}

		body, err := json.Marshal(question)

		assert.NoError(t, err)
		assert.Equal(t, `{"type":"noul","instructions":{"task":"Find spam"}}`, string(body))
	})
}

func TestChoiceQuestion_MarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("encodes instructions and criteria", func(t *testing.T) {
		t.Parallel()
		question := typesafe.ChoiceQuestion{
			Instructions: "Which team should handle this?",
			Criteria:     typesafe.ChoiceCriteria{"billing": "Payments", "other": nil},
		}

		body, err := json.Marshal(question)

		assert.NoError(t, err)
		assert.Equal(t, `{"type":"choice","instructions":"Which team should handle this?",`+
			`"criteria":{"billing":"Payments","other":null}}`, string(body))
	})
}

func TestScoreQuestion_MarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("encodes criteria in level order", func(t *testing.T) {
		t.Parallel()
		question := typesafe.ScoreQuestion{
			Instructions: "How frustrated is the customer?",
			Criteria:     typesafe.ScoreCriteria{"Calm", "Frustrated", "Very angry"},
		}

		body, err := json.Marshal(question)

		assert.NoError(t, err)
		assert.Equal(t, `{"type":"score","instructions":"How frustrated is the customer?",`+
			`"criteria":["Calm","Frustrated","Very angry"]}`, string(body))
	})
}

func TestRawQuestion_MarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("returns the question's json unchanged", func(t *testing.T) {
		t.Parallel()
		question := typesafe.RawQuestion{Type: "rank", JSON: json.RawMessage(`{"type":"rank","by":"date"}`)}

		body, err := json.Marshal(question)

		assert.NoError(t, err)
		assert.Equal(t, `{"type":"rank","by":"date"}`, string(body))
	})
}

func TestQuestions_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("decodes each question into the type its discriminator names", func(t *testing.T) {
		t.Parallel()
		body := `{
			"urgent": {"type": "noul", "instructions": "Urgent?",
				"criteria": {"true": "Yes", "false": "No"}},
			"department": {"type": "choice", "instructions": "Which team?",
				"criteria": {"billing": "Payments", "other": null}},
			"frustration": {"type": "score", "instructions": "How frustrated?",
				"criteria": ["Calm", "Angry"]}
		}`

		var questions typesafe.Questions
		err := json.Unmarshal([]byte(body), &questions)

		assert.NoError(t, err)
		assert.Equal(t, typesafe.Questions{
			"urgent": typesafe.NoulQuestion{
				Instructions: "Urgent?",
				Criteria:     &typesafe.NoulCriteria{True: "Yes", False: "No"},
			},
			"department": typesafe.ChoiceQuestion{
				Instructions: "Which team?",
				Criteria:     typesafe.ChoiceCriteria{"billing": "Payments", "other": nil},
			},
			"frustration": typesafe.ScoreQuestion{
				Instructions: "How frustrated?",
				Criteria:     typesafe.ScoreCriteria{"Calm", "Angry"},
			},
		}, questions)
	})

	t.Run("decodes a noul question without criteria", func(t *testing.T) {
		t.Parallel()

		var questions typesafe.Questions
		err := json.Unmarshal([]byte(`{"urgent":{"type":"noul"}}`), &questions)

		assert.NoError(t, err)
		assert.Equal[typesafe.Question](t, typesafe.NoulQuestion{}, questions["urgent"])
	})

	t.Run("keeps a question type it does not model", func(t *testing.T) {
		t.Parallel()

		var questions typesafe.Questions
		err := json.Unmarshal([]byte(`{"rank":{"type":"rank","by":"date"}}`), &questions)

		assert.NoError(t, err)
		assert.Equal[typesafe.Question](t, typesafe.RawQuestion{
			Type: "rank",
			JSON: json.RawMessage(`{"type":"rank","by":"date"}`),
		}, questions["rank"])
	})

	t.Run("round trips through the wire format", func(t *testing.T) {
		t.Parallel()
		want := ticketQuestions()

		body, err := json.Marshal(want)
		assert.NoError(t, err)
		var got typesafe.Questions
		assert.NoError(t, json.Unmarshal(body, &got))

		assert.Equal(t, want, got)
	})

	t.Run("reports a malformed question map", func(t *testing.T) {
		t.Parallel()

		var questions typesafe.Questions
		err := json.Unmarshal([]byte(`[]`), &questions)

		assert.Error(t, err)
	})

	t.Run("reports a malformed discriminator", func(t *testing.T) {
		t.Parallel()

		var questions typesafe.Questions
		err := json.Unmarshal([]byte(`{"urgent":{"type":7}}`), &questions)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "urgent")
	})

	t.Run("reports a malformed question", func(t *testing.T) {
		t.Parallel()
		bodies := map[string]string{
			"noul":   `{"urgent":{"type":"noul","criteria":7}}`,
			"choice": `{"department":{"type":"choice","criteria":[]}}`,
			"score":  `{"frustration":{"type":"score","criteria":{}}}`,
		}
		for name, body := range bodies {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				var questions typesafe.Questions
				err := json.Unmarshal([]byte(body), &questions)

				assert.Error(t, err)
			})
		}
	})
}
