package quiz

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"
)

func TestQuestionScoring_TableDriven(t *testing.T) {
	tests := []struct {
		name               string
		question           Question
		answers            []int
		allowPartialCredit bool
		expectedScore      int
	}{
		// Single Choice Tests
		{
			name: "single choice correct answer",
			question: Question{
				ID:             "q1",
				Text:           "Capital of France?",
				Type:           TypeSingleChoice,
				Options:        []string{"Berlin", "Paris", "Rome", "Madrid"},
				CorrectAnswers: []int{1},
				Points:         5,
			},
			answers:            []int{1},
			allowPartialCredit: false,
			expectedScore:      5,
		},
		{
			name: "single choice wrong answer",
			question: Question{
				ID:             "q1",
				Text:           "Capital of France?",
				Type:           TypeSingleChoice,
				Options:        []string{"Berlin", "Paris", "Rome", "Madrid"},
				CorrectAnswers: []int{1},
				Points:         5,
			},
			answers:            []int{0},
			allowPartialCredit: false,
			expectedScore:      0,
		},
		{
			name: "single choice multiple answers submitted (invalid)",
			question: Question{
				ID:             "q1",
				Text:           "Capital of France?",
				Type:           TypeSingleChoice,
				Options:        []string{"Berlin", "Paris", "Rome", "Madrid"},
				CorrectAnswers: []int{1},
				Points:         5,
			},
			answers:            []int{1, 2},
			allowPartialCredit: false,
			expectedScore:      0,
		},
		{
			name: "single choice empty answer",
			question: Question{
				ID:             "q1",
				Text:           "Capital of France?",
				Type:           TypeSingleChoice,
				Options:        []string{"Berlin", "Paris", "Rome", "Madrid"},
				CorrectAnswers: []int{1},
				Points:         5,
			},
			answers:            []int{},
			allowPartialCredit: false,
			expectedScore:      0,
		},

		// Multiple Choice Tests
		{
			name: "multiple choice all correct full credit",
			question: Question{
				ID:             "q2",
				Text:           "Select prime numbers:",
				Type:           TypeMultipleChoice,
				Options:        []string{"2", "3", "4", "5"},
				CorrectAnswers: []int{0, 1, 3},
				Points:         6,
			},
			answers:            []int{0, 1, 3},
			allowPartialCredit: false,
			expectedScore:      6,
		},
		{
			name: "multiple choice partial credit allowed (2 of 3 correct)",
			question: Question{
				ID:             "q2",
				Text:           "Select prime numbers:",
				Type:           TypeMultipleChoice,
				Options:        []string{"2", "3", "4", "5"},
				CorrectAnswers: []int{0, 1, 3},
				Points:         6,
			},
			answers:            []int{0, 1},
			allowPartialCredit: true,
			expectedScore:      4, // (6 * 2) / 3 = 4
		},
		{
			name: "multiple choice partial credit disabled (2 of 3 correct)",
			question: Question{
				ID:             "q2",
				Text:           "Select prime numbers:",
				Type:           TypeMultipleChoice,
				Options:        []string{"2", "3", "4", "5"},
				CorrectAnswers: []int{0, 1, 3},
				Points:         6,
			},
			answers:            []int{0, 1},
			allowPartialCredit: false,
			expectedScore:      0,
		},
		{
			name: "multiple choice with wrong option selected (penalty 0 points)",
			question: Question{
				ID:             "q2",
				Text:           "Select prime numbers:",
				Type:           TypeMultipleChoice,
				Options:        []string{"2", "3", "4", "5"},
				CorrectAnswers: []int{0, 1, 3},
				Points:         6,
			},
			answers:            []int{0, 1, 2}, // 2 is option index 2 ("4" is not prime)
			allowPartialCredit: true,
			expectedScore:      0,
		},
		{
			name: "multiple choice with duplicate answers in input",
			question: Question{
				ID:             "q2",
				Text:           "Select prime numbers:",
				Type:           TypeMultipleChoice,
				Options:        []string{"2", "3", "4", "5"},
				CorrectAnswers: []int{0, 1, 3},
				Points:         6,
			},
			answers:            []int{0, 0, 1, 1},
			allowPartialCredit: true,
			expectedScore:      4,
		},

		// True/False Tests
		{
			name: "true/false correct answer",
			question: Question{
				ID:             "q3",
				Text:           "The earth is round.",
				Type:           TypeTrueFalse,
				Options:        []string{"True", "False"},
				CorrectAnswers: []int{0},
				Points:         3,
			},
			answers:            []int{0},
			allowPartialCredit: false,
			expectedScore:      3,
		},
		{
			name: "true/false wrong answer",
			question: Question{
				ID:             "q3",
				Text:           "The earth is round.",
				Type:           TypeTrueFalse,
				Options:        []string{"True", "False"},
				CorrectAnswers: []int{0},
				Points:         3,
			},
			answers:            []int{1},
			allowPartialCredit: false,
			expectedScore:      0,
		},

		// Text Question Tests
		{
			name: "text question default zero auto-grade",
			question: Question{
				ID:             "q4",
				Text:           "Describe polymorphism.",
				Type:           TypeText,
				Points:         10,
				CorrectAnswers: nil,
			},
			answers:            []int{0},
			allowPartialCredit: false,
			expectedScore:      0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			score := tc.question.Grade(tc.answers, tc.allowPartialCredit)
			if score != tc.expectedScore {
				t.Fatalf("score mismatch: expected %d, got %d", tc.expectedScore, score)
			}
		})
	}
}

func TestQuizValidation(t *testing.T) {
	validQuiz := Quiz{
		ID:    "quiz-1",
		Title: "Golang Basics",
		Questions: []Question{
			{
				ID:             "q1",
				Text:           "Is Go garbage collected?",
				Type:           TypeTrueFalse,
				Options:        []string{"True", "False"},
				CorrectAnswers: []int{0},
				Points:         10,
			},
		},
	}
	if err := validQuiz.Validate(); err != nil {
		t.Fatalf("expected valid quiz, got error: %v", err)
	}

	// Missing ID
	badQuiz := validQuiz
	badQuiz.ID = ""
	if err := badQuiz.Validate(); err == nil {
		t.Fatal("expected error on empty quiz id, got nil")
	}

	// No questions
	badQuiz = validQuiz
	badQuiz.Questions = nil
	if err := badQuiz.Validate(); err == nil {
		t.Fatal("expected error on empty questions, got nil")
	}

	// Duplicate question ID
	badQuiz = validQuiz
	badQuiz.Questions = []Question{
		validQuiz.Questions[0],
		validQuiz.Questions[0],
	}
	if err := badQuiz.Validate(); err == nil {
		t.Fatal("expected error on duplicate question id, got nil")
	}
}

func TestQuizSessionLifecycleAndStats(t *testing.T) {
	qz := Quiz{
		ID:    "exam-101",
		Title: "Midterm Exam",
		Questions: []Question{
			{
				ID:             "q1",
				Text:           "What is 2+2?",
				Type:           TypeSingleChoice,
				Options:        []string{"3", "4", "5"},
				CorrectAnswers: []int{1}, // "4"
				Points:         10,
			},
			{
				ID:             "q2",
				Text:           "Which are even numbers?",
				Type:           TypeMultipleChoice,
				Options:        []string{"1", "2", "3", "4"},
				CorrectAnswers: []int{1, 3}, // "2", "4"
				Points:         10,
			},
		},
		TimeLimitSeconds: 300,
	}

	session := NewQuizSession(qz, true)

	if session.State() != StatePending {
		t.Fatalf("expected pending state, got %s", session.State())
	}

	// Submitting before start must fail
	_, err := session.Submit(Submission{
		StudentID: "s1",
		DeviceID:  "dev-1",
		Answers:   map[string][]int{"q1": {1}},
	})
	if err != ErrQuizNotActive {
		t.Fatalf("expected ErrQuizNotActive, got %v", err)
	}

	// Start session
	if err := session.Start(); err != nil {
		t.Fatalf("failed to start session: %v", err)
	}
	if session.State() != StateActive {
		t.Fatalf("expected active state, got %s", session.State())
	}

	// Student 1: 100% correct (Score: 10 + 10 = 20)
	sub1, err := session.Submit(Submission{
		StudentID:   "student-1",
		DeviceID:    "dev-1",
		StudentName: "Alice",
		Answers: map[string][]int{
			"q1": {1},
			"q2": {1, 3},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error submitting: %v", err)
	}
	if sub1.Score != 20 || sub1.MaxScore != 20 {
		t.Fatalf("expected score 20/20, got %d/%d", sub1.Score, sub1.MaxScore)
	}

	// Student 2: Partial credit on Q2 (Score: 10 + 5 = 15)
	sub2, err := session.Submit(Submission{
		StudentID:   "student-2",
		DeviceID:    "dev-2",
		StudentName: "Bob",
		Answers: map[string][]int{
			"q1": {1},
			"q2": {1}, // partial: 1 of 2 correct
		},
	})
	if err != nil {
		t.Fatalf("unexpected error submitting: %v", err)
	}
	if sub2.Score != 15 {
		t.Fatalf("expected score 15, got %d", sub2.Score)
	}

	// Student 3: Completely wrong (Score: 0)
	sub3, err := session.Submit(Submission{
		StudentID:   "student-3",
		DeviceID:    "dev-3",
		StudentName: "Charlie",
		Answers: map[string][]int{
			"q1": {0},       // wrong
			"q2": {0, 1, 3}, // includes wrong option 0
		},
	})
	if err != nil {
		t.Fatalf("unexpected error submitting: %v", err)
	}
	if sub3.Score != 0 {
		t.Fatalf("expected score 0, got %d", sub3.Score)
	}

	// Verify duplicate submission rejection
	_, err = session.Submit(Submission{
		StudentID: "student-1",
		DeviceID:  "dev-1",
	})
	if err != ErrDuplicateSubmission {
		t.Fatalf("expected ErrDuplicateSubmission, got %v", err)
	}

	// Verify GetSubmissions ordering and count
	subs := session.GetSubmissions()
	if len(subs) != 3 {
		t.Fatalf("expected 3 submissions, got %d", len(subs))
	}

	// Verify Real-time Statistics
	stats := session.CalculateStats()
	if stats.TotalSubmissions != 3 {
		t.Fatalf("expected 3 total submissions in stats, got %d", stats.TotalSubmissions)
	}
	// Scores: 20, 15, 0 -> Average: 35 / 3 = 11.6666...
	expectedAvg := 35.0 / 3.0
	if stats.ClassAverage < expectedAvg-0.01 || stats.ClassAverage > expectedAvg+0.01 {
		t.Fatalf("expected average ~%.2f, got %.2f", expectedAvg, stats.ClassAverage)
	}
	if stats.HighestScore != 20 || stats.LowestScore != 0 {
		t.Fatalf("score range mismatch: highest=%d, lowest=%d", stats.HighestScore, stats.LowestScore)
	}

	// Q1 Stats: Alice (1), Bob (1), Charlie (0). 2 correct out of 3.
	q1Stats := stats.Questions["q1"]
	if q1Stats.OptionCounts[1] != 2 || q1Stats.OptionCounts[0] != 1 {
		t.Fatalf("q1 option counts mismatch: %+v", q1Stats.OptionCounts)
	}
	expectedQ1Acc := (2.0 / 3.0) * 100.0
	if q1Stats.AccuracyPercentage < expectedQ1Acc-0.01 || q1Stats.AccuracyPercentage > expectedQ1Acc+0.01 {
		t.Fatalf("q1 accuracy mismatch: expected ~%.2f%%, got %.2f%%", expectedQ1Acc, q1Stats.AccuracyPercentage)
	}

	// End Session
	if err := session.End(); err != nil {
		t.Fatalf("failed to end session: %v", err)
	}
	if session.State() != StateEnded {
		t.Fatalf("expected ended state, got %s", session.State())
	}

	// Submitting after end must fail
	_, err = session.Submit(Submission{
		StudentID: "student-4",
		DeviceID:  "dev-4",
	})
	if err != ErrQuizNotActive {
		t.Fatalf("expected ErrQuizNotActive after end, got %v", err)
	}
}

func TestQuizSessionTimeLimitExpiration(t *testing.T) {
	qz := Quiz{
		ID:    "timed-quiz",
		Title: "Rapid Fire",
		Questions: []Question{
			{
				ID:             "q1",
				Text:           "True or False?",
				Type:           TypeTrueFalse,
				Options:        []string{"True", "False"},
				CorrectAnswers: []int{0},
				Points:         5,
			},
		},
		TimeLimitSeconds: 1, // 1 second limit
	}

	session := NewQuizSession(qz, false)
	if err := session.Start(); err != nil {
		t.Fatal(err)
	}

	// Immediate submission succeeds
	_, err := session.Submit(Submission{
		StudentID: "s1",
		DeviceID:  "d1",
		Answers:   map[string][]int{"q1": {0}},
	})
	if err != nil {
		t.Fatalf("expected immediate submit to pass, got: %v", err)
	}

	// Wait for expiration
	time.Sleep(1100 * time.Millisecond)

	// Submission after expiration should fail
	_, err = session.Submit(Submission{
		StudentID: "s2",
		DeviceID:  "d2",
		Answers:   map[string][]int{"q1": {0}},
	})
	if err != ErrQuizTimeExpired {
		t.Fatalf("expected ErrQuizTimeExpired, got %v", err)
	}

	if session.State() != StateEnded {
		t.Fatalf("expected session state to become ended after expiry, got: %s", session.State())
	}
}

func TestBuzzerSession_ConcurrencyRace(t *testing.T) {
	buzzer := NewBuzzerSession()
	buzzer.Start("Who invented Go at Google?")

	// Before start: buzzing when already started works, but let's test a fresh idle buzzer first
	idleBuzzer := NewBuzzerSession("test prompt")
	_, _, err := idleBuzzer.Buzz("dev-pre", "Pre Student")
	if err != ErrBuzzerNotActive {
		t.Fatalf("expected ErrBuzzerNotActive on idle buzzer, got %v", err)
	}

	const studentCount = 100
	var wg sync.WaitGroup
	wg.Add(studentCount)

	type buzzResult struct {
		rank    int
		deltaMs int64
		err     error
	}

	results := make([]buzzResult, studentCount)

	for i := 0; i < studentCount; i++ {
		idx := i
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)
			deviceID := fmt.Sprintf("dev-%03d", idx)
			studentName := fmt.Sprintf("Student-%03d", idx)

			rank, delta, err := buzzer.Buzz(deviceID, studentName)
			results[idx] = buzzResult{rank: rank, deltaMs: delta, err: err}
		}()
	}

	wg.Wait()

	// Verify all 100 students succeeded
	ranksSeen := make(map[int]bool)
	for i, res := range results {
		if res.err != nil {
			t.Fatalf("student %d failed to buzz: %v", i, res.err)
		}
		if res.rank < 1 || res.rank > studentCount {
			t.Fatalf("student %d got invalid rank %d", i, res.rank)
		}
		if ranksSeen[res.rank] {
			t.Fatalf("duplicate rank detected: %d", res.rank)
		}
		ranksSeen[res.rank] = true

		if res.deltaMs < 0 {
			t.Fatalf("student %d got negative deltaMs: %d", i, res.deltaMs)
		}
	}

	// Verify exactly 100 strictly consecutive ranks: 1, 2, ..., 100
	for r := 1; r <= studentCount; r++ {
		if !ranksSeen[r] {
			t.Fatalf("missing expected rank %d in sequence", r)
		}
	}

	// Verify duplicate buzz from same device is rejected
	_, _, err = buzzer.Buzz("dev-001", "Student-001 Duplicate")
	if err != ErrAlreadyBuzzed {
		t.Fatalf("expected ErrAlreadyBuzzed, got %v", err)
	}

	// Verify ordered winners list via both Winners() and Rankings()
	winners := buzzer.Winners()
	if len(winners) != studentCount {
		t.Fatalf("expected %d winners, got %d", studentCount, len(winners))
	}
	rankings := buzzer.Rankings()
	if len(rankings) != studentCount {
		t.Fatalf("expected %d rankings, got %d", studentCount, len(rankings))
	}
	for i, w := range winners {
		if w.Rank != i+1 {
			t.Fatalf("winner at index %d has rank %d, expected %d", i, w.Rank, i+1)
		}
	}

	// Close buzzer
	if err := buzzer.Close(); err != nil {
		t.Fatalf("failed to close buzzer: %v", err)
	}
	if buzzer.State() != BuzzerClosed {
		t.Fatalf("expected BuzzerClosed state, got %s", buzzer.State())
	}

	// Buzzing after close should fail
	_, _, err = buzzer.Buzz("dev-late", "Late Student")
	if err != ErrBuzzerNotActive {
		t.Fatalf("expected ErrBuzzerNotActive after close, got %v", err)
	}

	// Reset
	buzzer.Reset()
	if buzzer.State() != BuzzerIdle {
		t.Fatalf("expected BuzzerIdle after reset, got %s", buzzer.State())
	}
	if len(buzzer.Winners()) != 0 {
		t.Fatalf("expected 0 winners after reset, got %d", len(buzzer.Winners()))
	}
}

func TestBuzzerSession_MaxWinnersLimit(t *testing.T) {
	buzzer := NewBuzzerSession("Top 3 students only")
	buzzer.SetMaxWinners(3)
	if err := buzzer.Start(); err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 3; i++ {
		rank, _, err := buzzer.Buzz(fmt.Sprintf("dev-%d", i), fmt.Sprintf("Student %d", i))
		if err != nil {
			t.Fatalf("buzz %d failed: %v", i, err)
		}
		if rank != i {
			t.Fatalf("expected rank %d, got %d", i, rank)
		}
	}

	// 4th student should be rejected because limit is reached and session auto-closes
	_, _, err := buzzer.Buzz("dev-4", "Student 4")
	if err != ErrBuzzerNotActive && err != ErrBuzzerLimitReached {
		t.Fatalf("expected rejection when max winners reached, got %v", err)
	}

	if buzzer.State() != BuzzerClosed {
		t.Fatalf("expected session to auto-close after 3 winners, got %s", buzzer.State())
	}
}
