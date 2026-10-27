package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"exam-platform/internal/domain"
)

// CreateClass stores a class and its students in one transaction. A student already known from another class keeps one
// record, updated from this roster. The name must not be taken by another class, ignoring case.
func (s *Store) CreateClass(ctx context.Context, class domain.Class, students []domain.Student) error {
	if len(students) == 0 {
		return fmt.Errorf("cannot create an empty class")
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var taken bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM classes WHERE name = ? COLLATE NOCASE)`, class.Name).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return domain.ErrClassExists
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO classes(class_id, name, created_at) VALUES (?, ?, ?)`, class.ID, class.Name, timestamp(class.CreatedAt)); err != nil {
			return err
		}
		for _, student := range students {
			if _, err := tx.ExecContext(ctx, `INSERT INTO students (account_number, canvas_id, name, email, sis_user_id) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT(account_number) DO UPDATE SET canvas_id = excluded.canvas_id, name = excluded.name,
				email = excluded.email, sis_user_id = excluded.sis_user_id`,
				student.AccountNumber, student.CanvasID, student.Name, student.Email, student.SISUserID); err != nil {
				return fmt.Errorf("store student: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO class_students(class_id, account_number) VALUES (?, ?)`, class.ID, student.AccountNumber); err != nil {
				return fmt.Errorf("add student to class: %w", err)
			}
		}
		return nil
	})
}

// ListClasses returns every class by name with its size, how many exams it has and the exam students are being admitted
// to when that one is in this class.
func (s *Store) ListClasses(ctx context.Context) ([]domain.ClassSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.class_id, c.name, c.created_at,
			(SELECT count(*) FROM class_students cs WHERE cs.class_id = c.class_id),
			(SELECT count(*) FROM exams e WHERE e.class_id = c.class_id),
			p.exam_id, p.title
		FROM classes c
		LEFT JOIN (SELECT e.exam_id, e.title, e.class_id FROM current_exam ce JOIN exams e ON e.exam_id = ce.exam_id) p ON p.class_id = c.class_id
		ORDER BY c.name COLLATE NOCASE, c.class_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	classes := []domain.ClassSummary{}
	for rows.Next() {
		var summary domain.ClassSummary
		var created string
		var examID, title sql.NullString
		if err := rows.Scan(&summary.ID, &summary.Name, &created, &summary.StudentCount, &summary.ExamCount, &examID, &title); err != nil {
			return nil, err
		}
		if summary.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, err
		}
		if examID.Valid {
			summary.PublishedExam = &domain.ClassExam{ExamID: examID.String, Title: title.String}
		}
		classes = append(classes, summary)
	}
	return classes, rows.Err()
}

func scanClass(row *sql.Row) (domain.Class, error) {
	var class domain.Class
	var created string
	err := row.Scan(&class.ID, &class.Name, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Class{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Class{}, err
	}
	class.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	return class, err
}

func (s *Store) Class(ctx context.Context, id string) (domain.Class, error) {
	return scanClass(s.db.QueryRowContext(ctx, `SELECT class_id, name, created_at FROM classes WHERE class_id = ?`, id))
}

// ClassStudents lists a class's students by account number.
func (s *Store) ClassStudents(ctx context.Context, id string) ([]domain.Student, error) {
	if _, err := s.Class(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT s.account_number, s.canvas_id, s.name, s.email, s.sis_user_id FROM class_students cs
		JOIN students s ON s.account_number = cs.account_number WHERE cs.class_id = ? ORDER BY s.account_number`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	students := []domain.Student{}
	for rows.Next() {
		var student domain.Student
		if err := rows.Scan(&student.AccountNumber, &student.CanvasID, &student.Name, &student.Email, &student.SISUserID); err != nil {
			return nil, err
		}
		students = append(students, student)
	}
	return students, rows.Err()
}

func (t *sessionTransaction) StudentInClass(ctx context.Context, classID, account string) (bool, error) {
	var member bool
	err := t.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM class_students WHERE class_id = ? AND account_number = ?)`, classID, account).Scan(&member)
	return member, err
}

func (t *publishTransaction) Class(ctx context.Context, id string) (domain.Class, error) {
	return scanClass(t.tx.QueryRowContext(ctx, `SELECT class_id, name, created_at FROM classes WHERE class_id = ?`, id))
}

// RenameClass changes a class's name. The new name must not belong to another class, ignoring case, so only changing the
// case of its own name is allowed.
func (s *Store) RenameClass(ctx context.Context, id, name string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var taken bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM classes WHERE name = ? COLLATE NOCASE AND class_id <> ?)`, name, id).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return domain.ErrClassExists
		}
		result, err := tx.ExecContext(ctx, `UPDATE classes SET name = ? WHERE class_id = ?`, name, id)
		if err != nil {
			return err
		}
		if changed, err := result.RowsAffected(); err != nil || changed == 0 {
			if err != nil {
				return err
			}
			return domain.ErrNotFound
		}
		return nil
	})
}

// ClassAdmitting says whether the class is the one students are being admitted from: the one the published exam belongs to.
func (s *Store) ClassAdmitting(ctx context.Context, id string) (bool, error) {
	var admitting bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM current_exam ce JOIN exams e ON e.exam_id = ce.exam_id WHERE e.class_id = ?)`, id).Scan(&admitting)
	return admitting, err
}

// classAccounts lists the accounts of a class's students.
func classAccounts(ctx context.Context, tx *sql.Tx, id string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT account_number FROM class_students WHERE class_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []string
	for rows.Next() {
		var account string
		if err := rows.Scan(&account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

// pruneStudents removes the people among accounts who now belong to no class and have never logged in. Anyone with a session
// stays, because the history of that exam shows their name.
func pruneStudents(ctx context.Context, tx *sql.Tx, accounts []string) (int, error) {
	pruned := 0
	for _, account := range accounts {
		result, err := tx.ExecContext(ctx, `DELETE FROM students WHERE account_number = ?
			AND NOT EXISTS (SELECT 1 FROM class_students WHERE class_students.account_number = students.account_number)
			AND NOT EXISTS (SELECT 1 FROM sessions WHERE sessions.account_number = students.account_number)`, account)
		if err != nil {
			return 0, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		pruned += int(changed)
	}
	return pruned, nil
}

// DeleteClass removes a class and its roster. It refuses a class that has exams (domain.ErrClassHasExams): exams stay with
// their class for good, and the ones students have taken cannot be deleted. Sessions and answers are untouched.
func (s *Store) DeleteClass(ctx context.Context, id string) (domain.DeletedClass, error) {
	var deleted domain.DeletedClass
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		deleted = domain.DeletedClass{ID: id}
		if err := tx.QueryRowContext(ctx, `SELECT name FROM classes WHERE class_id = ?`, id).Scan(&deleted.Name); errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		} else if err != nil {
			return err
		}
		var hasExams bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exams WHERE class_id = ?)`, id).Scan(&hasExams); err != nil {
			return err
		}
		if hasExams {
			return domain.ErrClassHasExams
		}
		accounts, err := classAccounts(ctx, tx, id)
		if err != nil {
			return err
		}
		deleted.Students = len(accounts)
		for _, statement := range []string{`DELETE FROM class_students WHERE class_id = ?`, `DELETE FROM classes WHERE class_id = ?`} {
			if _, err := tx.ExecContext(ctx, statement, id); err != nil {
				return err
			}
		}
		deleted.Pruned, err = pruneStudents(ctx, tx, accounts)
		return err
	})
	if err != nil {
		return domain.DeletedClass{}, err
	}
	return deleted, nil
}

// ReplaceClassRoster makes the class hold exactly the given students. People already known keep one record that the new data
// updates; students who are no longer in the class lose their place in it only.
func (s *Store) ReplaceClassRoster(ctx context.Context, id string, students []domain.Student) (domain.RosterReplacement, error) {
	if len(students) == 0 {
		return domain.RosterReplacement{}, fmt.Errorf("cannot replace a roster with an empty one")
	}
	var change domain.RosterReplacement
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		change = domain.RosterReplacement{}
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM classes WHERE class_id = ?)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return domain.ErrNotFound
		}
		members, err := classAccounts(ctx, tx, id)
		if err != nil {
			return err
		}
		inClass := make(map[string]bool, len(members))
		for _, account := range members {
			inClass[account] = true
		}
		inFile := make(map[string]bool, len(students))
		for _, student := range students {
			inFile[student.AccountNumber] = true
			if _, err := tx.ExecContext(ctx, `INSERT INTO students (account_number, canvas_id, name, email, sis_user_id) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT(account_number) DO UPDATE SET canvas_id = excluded.canvas_id, name = excluded.name,
				email = excluded.email, sis_user_id = excluded.sis_user_id`,
				student.AccountNumber, student.CanvasID, student.Name, student.Email, student.SISUserID); err != nil {
				return fmt.Errorf("store student: %w", err)
			}
			if !inClass[student.AccountNumber] {
				if _, err := tx.ExecContext(ctx, `INSERT INTO class_students(class_id, account_number) VALUES (?, ?)`, id, student.AccountNumber); err != nil {
					return fmt.Errorf("add student to class: %w", err)
				}
				change.Added++
			}
		}
		var left []string
		for _, account := range members {
			if !inFile[account] {
				if _, err := tx.ExecContext(ctx, `DELETE FROM class_students WHERE class_id = ? AND account_number = ?`, id, account); err != nil {
					return err
				}
				left = append(left, account)
			}
		}
		change.Removed = len(left)
		change.Pruned, err = pruneStudents(ctx, tx, left)
		return err
	})
	if err != nil {
		return domain.RosterReplacement{}, err
	}
	return change, nil
}
