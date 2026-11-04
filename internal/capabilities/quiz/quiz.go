package quiz

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrQuizNotActive       = errors.New("quiz session is not currently active")
	ErrQuizAlreadyStarted  = errors.New("quiz session is already active or ended")
	ErrSessionNotPending   = errors.New("quiz session is not in pending state")
	ErrSessionEnded        = errors.New("quiz session has already ended")
	ErrDuplicateSubmission = errors.New("submission already received for this student or device")
	ErrInvalidQuestion     = errors.New("invalid question structure")
	ErrInvalidQuizID       = errors.New("quiz id cannot be empty")
	ErrInvalidQuizTitle    = errors.New("quiz title cannot be empty")
	ErrEmptyQuestions      = errors.New("quiz must have at least one question")
	ErrQuizTimeExpired     = errors.New("quiz time limit has expired")
	ErrInvalidStudentID    = errors.New("student id is required")
	ErrInvalidDeviceID     = errors.New("device id is required")
)

// QuestionType represents the category of question.
type QuestionType string

const (
	TypeSingleChoice   QuestionType = "single_choice"
	TypeMultipleChoice QuestionType = "multiple_choice"
	TypeTrueFalse      QuestionType = "true_false"
	TypeText           QuestionType = "text"
)

// Question defines an individual quiz question.
type Question struct {
	ID             string       `json:"id"`
	Text           string       `json:"text"`
	Type           QuestionType `json:"type"`
	Options        []string     `json:"options,omitempty"`
	CorrectAnswers []int        `json:"correct_answers,omitempty"` // 0-based indices
	Points         int          `json:"points"`
}

// Validate checks question validity.
func (q Question) Validate() error {
	if strings.TrimSpace(q.ID) == "" {
		return fmt.Errorf("%w: empty question id", ErrInvalidQuestion)
	}
	if strings.TrimSpace(q.Text) == "" {
		return fmt.Errorf("%w: empty question text", ErrInvalidQuestion)
	}
	if q.Points <= 0 {
		return fmt.Errorf("%w: points must be greater than zero", ErrInvalidQuestion)
	}

	switch q.Type {
	case TypeSingleChoice:
		if len(q.Options) < 2 {
			return fmt.Errorf("%w: single choice requires at least 2 options", ErrInvalidQuestion)
		}
		if len(q.CorrectAnswers) != 1 || q.CorrectAnswers[0] < 0 || q.CorrectAnswers[0] >= len(q.Options) {
			return fmt.Errorf("%w: single choice must have exactly 1 valid correct answer", ErrInvalidQuestion)
		}
	case TypeMultipleChoice:
		if len(q.Options) < 2 {
			return fmt.Errorf("%w: multiple choice requires at least 2 options", ErrInvalidQuestion)
		}
		if len(q.CorrectAnswers) == 0 {
			return fmt.Errorf("%w: multiple choice requires at least 1 correct answer", ErrInvalidQuestion)
		}
		for _, ans := range q.CorrectAnswers {
			if ans < 0 || ans >= len(q.Options) {
				return fmt.Errorf("%w: correct answer index %d out of bounds", ErrInvalidQuestion, ans)
			}
		}
	case TypeTrueFalse:
		if len(q.Options) == 0 {
			q.Options = []string{"True", "False"}
		}
		if len(q.Options) != 2 {
			return fmt.Errorf("%w: true/false requires exactly 2 options", ErrInvalidQuestion)
		}
		if len(q.CorrectAnswers) != 1 || (q.CorrectAnswers[0] != 0 && q.CorrectAnswers[0] != 1) {
			return fmt.Errorf("%w: true/false must have 0 or 1 as correct answer", ErrInvalidQuestion)
		}
	case TypeText:
		// Text question may not have predefined options
	default:
		return fmt.Errorf("%w: unknown type %q", ErrInvalidQuestion, q.Type)
	}
	return nil
}

// Grade evaluates student answers against the question's correct answers.
func (q Question) Grade(answers []int, allowPartialCredit bool) int {
	pts := q.Points
	if pts <= 0 {
		pts = 10
	}

	if len(answers) == 0 {
		return 0
	}

	switch q.Type {
	case TypeSingleChoice, TypeTrueFalse:
		if len(answers) == 1 && len(q.CorrectAnswers) == 1 && answers[0] == q.CorrectAnswers[0] {
			return pts
		}
		return 0

	case TypeMultipleChoice:
		if len(q.CorrectAnswers) == 0 {
			return 0
		}

		correctMap := make(map[int]bool)
		for _, ca := range q.CorrectAnswers {
			correctMap[ca] = true
		}

		studentMap := make(map[int]bool)
		for _, ans := range answers {
			studentMap[ans] = true
		}

		// False positive penalty: selecting any wrong option yields 0 points
		for ans := range studentMap {
			if !correctMap[ans] {
				return 0
			}
		}

		// Full credit: all correct answers chosen, no wrong answers
		if len(studentMap) == len(correctMap) {
			return pts
		}

		// Partial credit if allowed
		if allowPartialCredit && len(studentMap) > 0 && len(studentMap) < len(correctMap) {
			partial := (pts * len(studentMap)) / len(correctMap)
			if partial < 1 && pts > 0 {
				partial = 1
			}
			return partial
		}
		return 0

	case TypeText:
		if len(q.CorrectAnswers) == 1 && len(answers) == 1 && answers[0] == q.CorrectAnswers[0] {
			return pts
		}
		return 0

	default:
		return 0
	}
}

// Quiz represents a complete exam or survey definition.
type Quiz struct {
	ID               string     `json:"id"`
	Title            string     `json:"title"`
	Description      string     `json:"description,omitempty"`
	Questions        []Question `json:"questions"`
	TimeLimitSeconds int        `json:"time_limit_seconds,omitempty"`
}

// Validate checks quiz structure.
func (qz Quiz) Validate() error {
	if strings.TrimSpace(qz.ID) == "" {
		return ErrInvalidQuizID
	}
	if strings.TrimSpace(qz.Title) == "" {
		return ErrInvalidQuizTitle
	}
	if len(qz.Questions) == 0 {
		return ErrEmptyQuestions
	}

	ids := make(map[string]bool)
	for _, q := range qz.Questions {
		if err := q.Validate(); err != nil {
			return err
		}
		if ids[q.ID] {
			return fmt.Errorf("duplicate question id: %s", q.ID)
		}
		ids[q.ID] = true
	}
	return nil
}

// TotalPoints sums the points across all questions.
func (qz Quiz) TotalPoints() int {
	total := 0
	for _, q := range qz.Questions {
		pts := q.Points
		if pts <= 0 {
			pts = 10
		}
		total += pts
	}
	return total
}

// Submission records a student's responses and evaluated score.
type Submission struct {
	StudentID   string           `json:"student_id"`
	DeviceID    string           `json:"device_id"`
	StudentName string           `json:"student_name"`
	Answers     map[string][]int `json:"answers"` // QuestionID -> chosen option indices
	// TextAnswers carries free-text answers keyed by question id; graded as
	// "answered" (non-empty) with teacher review afterwards.
	TextAnswers map[string]string `json:"text_answers,omitempty"`
	SubmittedAt time.Time         `json:"submitted_at"`
	Score       int               `json:"score"`
	MaxScore    int               `json:"max_score"`
}

// SessionState models the quiz progression lifecycle.
type SessionState string

const (
	StatePending SessionState = "pending"
	StateActive  SessionState = "active"
	StateEnded   SessionState = "ended"
)

// QuestionStats holds aggregated metrics for a single question.
type QuestionStats struct {
	QuestionID         string      `json:"question_id"`
	TotalAnswers       int         `json:"total_answers"`
	TotalSubmissions   int         `json:"total_submissions"`
	CorrectCount       int         `json:"correct_count"`
	CorrectSubmissions int         `json:"correct_submissions"`
	Accuracy           float64     `json:"accuracy_percent"`
	AccuracyPercentage float64     `json:"accuracy_percentage"`
	OptionCounts       map[int]int `json:"option_counts"` // option index -> selection count
}

// QuizStats holds real-time summary statistics for a quiz session.
type QuizStats struct {
	QuizID             string                   `json:"quiz_id"`
	TotalParticipants  int                      `json:"total_participants"`
	TotalSubmissions   int                      `json:"total_submissions"`
	AverageScore       float64                  `json:"average_score"`
	ClassAverage       float64                  `json:"class_average"`
	MaxPossibleScore   int                      `json:"max_possible_score"`
	HighestScore       int                      `json:"highest_score"`
	LowestScore        int                      `json:"lowest_score"`
	QuestionsAnalytics []QuestionStats          `json:"questions_analytics"`
	Questions          map[string]QuestionStats `json:"questions"`
}

// QuizSession manages quiz delivery, state machine, submissions, and analytics.
type QuizSession struct {
	mu                 sync.RWMutex
	quiz               Quiz
	state              SessionState
	startedAt          time.Time
	endedAt            time.Time
	allowPartialCredit bool
	submissions        map[string]*Submission // studentID -> Submission
	deviceSubmissions  map[string]*Submission // deviceID -> Submission
}

// NewQuizSession creates a new QuizSession in StatePending.
func NewQuizSession(q Quiz, allowPartialCredit ...bool) *QuizSession {
	partial := true
	if len(allowPartialCredit) > 0 {
		partial = allowPartialCredit[0]
	}
	return &QuizSession{
		quiz:               q,
		state:              StatePending,
		allowPartialCredit: partial,
		submissions:        make(map[string]*Submission),
		deviceSubmissions:  make(map[string]*Submission),
	}
}

// Start activates the quiz session for student submissions.
func (s *QuizSession) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == StateActive {
		return ErrQuizAlreadyStarted
	}
	if s.state == StateEnded {
		return ErrSessionEnded
	}

	s.state = StateActive
	s.startedAt = time.Now()
	return nil
}

// End closes the quiz session to further submissions.
func (s *QuizSession) End() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != StateActive {
		return ErrQuizNotActive
	}

	s.state = StateEnded
	s.endedAt = time.Now()
	return nil
}

// isExpiredLocked checks if time limit has elapsed. Caller must hold at least RLock.
func (s *QuizSession) isExpiredLocked() bool {
	if s.state == StateActive && s.quiz.TimeLimitSeconds > 0 && !s.startedAt.IsZero() {
		return time.Since(s.startedAt) > time.Duration(s.quiz.TimeLimitSeconds)*time.Second
	}
	return false
}

// State returns the current session state.
func (s *QuizSession) State() SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isExpiredLocked() {
		s.state = StateEnded
		if s.endedAt.IsZero() {
			s.endedAt = s.startedAt.Add(time.Duration(s.quiz.TimeLimitSeconds) * time.Second)
		}
	}
	return s.state
}

// GetState returns the current state of the session.
func (s *QuizSession) GetState() SessionState {
	return s.State()
}

// Submit records, auto-grades, and stores a student's submission.
func (s *QuizSession) Submit(sub Submission) (*Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isExpiredLocked() {
		s.state = StateEnded
		if s.endedAt.IsZero() {
			s.endedAt = s.startedAt.Add(time.Duration(s.quiz.TimeLimitSeconds) * time.Second)
		}
		return nil, ErrQuizTimeExpired
	}

	if s.state != StateActive {
		return nil, ErrQuizNotActive
	}

	trimmedStudentID := strings.TrimSpace(sub.StudentID)
	if trimmedStudentID == "" {
		return nil, ErrInvalidStudentID
	}

	if _, exists := s.submissions[trimmedStudentID]; exists {
		return nil, ErrDuplicateSubmission
	}

	trimmedDeviceID := strings.TrimSpace(sub.DeviceID)
	if trimmedDeviceID != "" {
		if _, exists := s.deviceSubmissions[trimmedDeviceID]; exists {
			return nil, ErrDuplicateSubmission
		}
	}

	// Grade submission. 文字题按"非空即得分"计（教师事后批阅调整）。
	score := 0
	maxScore := s.quiz.TotalPoints()
	for _, q := range s.quiz.Questions {
		if q.Type == TypeText {
			if len(strings.TrimSpace(sub.TextAnswers[q.ID])) > 0 {
				score += q.Points
			}
			continue
		}
		chosen := sub.Answers[q.ID]
		score += q.Grade(chosen, s.allowPartialCredit)
	}

	sub.Score = score
	sub.MaxScore = maxScore
	sub.SubmittedAt = time.Now()

	entry := sub
	s.submissions[trimmedStudentID] = &entry
	if trimmedDeviceID != "" {
		s.deviceSubmissions[trimmedDeviceID] = &entry
	}

	return &entry, nil
}

// GetSubmission retrieves a submission by student ID or device ID.
func (s *QuizSession) GetSubmission(id string) (*Submission, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if sub, ok := s.submissions[id]; ok {
		return sub, true
	}
	if sub, ok := s.deviceSubmissions[id]; ok {
		return sub, true
	}
	return nil, false
}

// GetSubmissions returns all recorded submissions sorted by student ID.
func (s *QuizSession) GetSubmissions() []*Submission {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Submission, 0, len(s.submissions))
	for _, sub := range s.submissions {
		out = append(out, sub)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StudentID < out[j].StudentID
	})
	return out
}

// CalculateStats compiles real-time question statistics, option counts, and class average.
func (s *QuizSession) CalculateStats() QuizStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := len(s.submissions)
	maxPts := s.quiz.TotalPoints()

	qStatsMap := make(map[string]*QuestionStats)
	for _, q := range s.quiz.Questions {
		qStatsMap[q.ID] = &QuestionStats{
			QuestionID:   q.ID,
			OptionCounts: make(map[int]int),
		}
	}

	if total == 0 {
		analytics := make([]QuestionStats, 0, len(s.quiz.Questions))
		qMap := make(map[string]QuestionStats)
		for _, q := range s.quiz.Questions {
			st := *qStatsMap[q.ID]
			analytics = append(analytics, st)
			qMap[q.ID] = st
		}
		return QuizStats{
			QuizID:             s.quiz.ID,
			MaxPossibleScore:   maxPts,
			QuestionsAnalytics: analytics,
			Questions:          qMap,
		}
	}

	sumScore := 0
	high := -1
	low := math.MaxInt

	for _, sub := range s.submissions {
		sumScore += sub.Score
		if sub.Score > high {
			high = sub.Score
		}
		if sub.Score < low {
			low = sub.Score
		}

		for _, q := range s.quiz.Questions {
			qs := qStatsMap[q.ID]
			qs.TotalAnswers++
			qs.TotalSubmissions++

			chosen := sub.Answers[q.ID]
			for _, c := range chosen {
				qs.OptionCounts[c]++
			}

			// Fully correct evaluation
			pts := q.Points
			if pts <= 0 {
				pts = 10
			}
			if q.Grade(chosen, false) == pts {
				qs.CorrectCount++
				qs.CorrectSubmissions++
			}
		}
	}

	analytics := make([]QuestionStats, 0, len(s.quiz.Questions))
	qMap := make(map[string]QuestionStats)
	for _, q := range s.quiz.Questions {
		qs := qStatsMap[q.ID]
		if qs.TotalAnswers > 0 {
			acc := (float64(qs.CorrectCount) / float64(qs.TotalAnswers)) * 100.0
			qs.Accuracy = acc
			qs.AccuracyPercentage = acc
		}
		analytics = append(analytics, *qs)
		qMap[q.ID] = *qs
	}

	avg := float64(sumScore) / float64(total)
	return QuizStats{
		QuizID:             s.quiz.ID,
		TotalParticipants:  total,
		TotalSubmissions:   total,
		AverageScore:       avg,
		ClassAverage:       avg,
		MaxPossibleScore:   maxPts,
		HighestScore:       high,
		LowestScore:        low,
		QuestionsAnalytics: analytics,
		Questions:          qMap,
	}
}

// GetStats is an alias for CalculateStats.
func (s *QuizSession) GetStats() QuizStats {
	return s.CalculateStats()
}

// Quiz returns the quiz associated with the session.
func (s *QuizSession) Quiz() Quiz {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.quiz
}

// RemainingSeconds returns seconds remaining or 0 if unlimited/ended.
func (s *QuizSession) RemainingSeconds() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.quiz.TimeLimitSeconds <= 0 || s.startedAt.IsZero() || s.state != StateActive {
		return 0
	}
	elapsed := int(time.Since(s.startedAt).Seconds())
	remaining := s.quiz.TimeLimitSeconds - elapsed
	if remaining < 0 {
		return 0
	}
	return remaining
}
