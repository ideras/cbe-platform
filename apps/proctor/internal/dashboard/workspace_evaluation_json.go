package dashboard

import (
	"encoding/json"
	"strconv"
	"strings"
)

type evaluationReport struct {
	Student  evaluationStudent   `json:"student"`
	Exam     evaluationExam      `json:"exam"`
	Sections []evaluationSection `json:"sections"`
}

type evaluationStudent struct {
	Name          string  `json:"name"`
	CanvasID      *int64  `json:"canvas_id"`
	AccountNumber *string `json:"account_number"`
	Gender        *string `json:"gender"`
}

type evaluationExam struct {
	Course          string   `json:"course"`
	Name            *string  `json:"name"`
	Description     *string  `json:"description"`
	Period          *string  `json:"period"`
	Score           float64  `json:"score"`
	TotalPoints     float64  `json:"total_points"`
	PromotedScore   *float64 `json:"promoted_score"`
	PromotionReason *string  `json:"promotion_reason"`
}

type evaluationCriterion struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Score       float64 `json:"score"`
	TotalPoints float64 `json:"total_points"`
}

type evaluationSection struct {
	Type string `json:"type"`
	evaluationCriterion
	Sections  []evaluationSection  `json:"sections"`
	Exercises []evaluationExercise `json:"exercises"`
}

type evaluationExercise struct {
	Type string `json:"type"`
	evaluationCriterion
	Exercises      []evaluationExercise `json:"exercises"`
	EvaluatedItems []evaluationItem     `json:"evaluated_items"`
}

type evaluationItem struct {
	evaluationCriterion
	Comment        string           `json:"comment"`
	EvaluatedItems []evaluationItem `json:"evaluated_items"`
}

// Workspace rubrics are flat. Each TODO is one evaluated item in a single exercise.
func renderStudentEvaluationJSON(exam ExamEntry, student Student, gradings WorkspaceGradings, run Grading, ticked map[string]bool) ([]byte, error) {
	report := evaluationReport{
		Student:  evaluationStudent{Name: student.Name},
		Exam:     evaluationExam{Course: exam.ClassName},
		Sections: []evaluationSection{},
	}
	if id, err := strconv.ParseInt(strings.TrimSpace(gradings.CanvasID), 10, 64); err == nil && id >= 0 {
		report.Student.CanvasID = &id
	}
	if student.Account != "" {
		report.Student.AccountNumber = &student.Account
	}
	if exam.Title != "" {
		report.Exam.Name = &exam.Title
	}

	items := make([]evaluationItem, 0, len(gradings.Todos))
	for _, row := range studentEvaluationRows(gradings.Todos, run, ticked) {
		item := evaluationItem{
			evaluationCriterion: evaluationCriterion{Name: row.todo.ID, Description: row.todo.Text},
			Comment:             row.verdict + ". Marked as done: " + row.tick + ".",
			EvaluatedItems:      []evaluationItem{},
		}
		if row.earned != nil {
			item.Score = *row.earned
		}
		if row.todo.Points != nil {
			item.TotalPoints = *row.todo.Points
		} else {
			item.Comment += " No maximum points were specified."
		}
		if strings.TrimSpace(row.feedback) != "" {
			item.Comment += "\n" + row.feedback
		}
		report.Exam.Score += item.Score
		report.Exam.TotalPoints += item.TotalPoints
		items = append(items, item)
	}
	// Prefer the recorded rubric totals, including zero. Older runs may only have item scores.
	if run.Points != nil {
		report.Exam.Score = *run.Points
	}
	if run.Max != nil {
		report.Exam.TotalPoints = *run.Max
	}
	if len(items) > 0 {
		exercise := evaluationExercise{
			Type:                "exercise",
			evaluationCriterion: evaluationCriterion{Name: "Workspace TODOs", Description: "TODO items in exam order.", Score: report.Exam.Score, TotalPoints: report.Exam.TotalPoints},
			Exercises:           []evaluationExercise{}, EvaluatedItems: items,
		}
		report.Sections = []evaluationSection{{
			Type:                "section",
			evaluationCriterion: evaluationCriterion{Name: "Grader feedback", Description: "Evaluation of the submitted workspace.", Score: report.Exam.Score, TotalPoints: report.Exam.TotalPoints},
			Sections:            []evaluationSection{}, Exercises: []evaluationExercise{exercise},
		}}
	}
	return json.MarshalIndent(report, "", "  ")
}
