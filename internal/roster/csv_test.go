package roster

import (
	"strings"
	"testing"
)

func TestLoadRoster(t *testing.T) {
	input := " Name , EMAIL , account_number , sis_user_id , canvas_id ,extra\n First , first@example.invalid , 0012 , S1 , 1 , x\n Blank , b@example.invalid , , S2 , 2 , x\n Last , last@example.invalid , 0012 , S3 , 3 , x\n Other , other@example.invalid , 0003 , S4 , 4 , x\n"
	result, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Students) != 2 || len(result.Warnings) != 2 {
		t.Fatalf("result = %+v", result)
	}
	student := result.Students[1]
	if student.AccountNumber != "0012" || student.Name != "Last" || student.CanvasID != "3" || student.Email != "last@example.invalid" || student.SISUserID != "S3" {
		t.Fatalf("student = %+v", student)
	}
	if result.Warnings[0].Row != 3 || result.Warnings[1].Row != 4 {
		t.Fatalf("warnings = %+v", result.Warnings)
	}
}

func TestLoadRosterMissingColumn(t *testing.T) {
	_, err := Parse(strings.NewReader("canvas_id,account_number,name,email\n1,0012,A,a@example.invalid\n"))
	if err == nil || !strings.Contains(err.Error(), "sis_user_id") {
		t.Fatalf("error = %v", err)
	}
}

func TestInvalidRoster(t *testing.T) {
	const header = "canvas_id,account_number,name,email,sis_user_id\n"
	for name, input := range map[string]string{
		"empty": "", "header only": header, "blank accounts": header + "1,,A,a@example.invalid,S\n",
		"short row": header + "1,0012,A\n", "long row": header + "1,0012,A,a@example.invalid,S,extra\n",
		"bad quotes":            header + "1,0012,\"A,a@example.invalid,S\n",
		"duplicate columns":     "canvas_id,account_number,name,email,sis_user_id,name\n",
		"malformed after valid": header + "1,0012,A,a@example.invalid,S\n2,0034,B\n",
	} {
		t.Run(name, func(t *testing.T) {
			result, err := Parse(strings.NewReader(input))
			if err == nil {
				t.Fatal("expected error")
			}
			if len(result.Students) != 0 {
				t.Fatal("partial roster returned")
			}
		})
	}
}

func TestLoadRosterAcceptsAByteOrderMark(t *testing.T) {
	result, err := Parse(strings.NewReader("\xef\xbb\xbfcanvas_id,account_number,name,email,sis_user_id\n1,0012,Ada,a@example.invalid,S1\n"))
	if err != nil || len(result.Students) != 1 || result.Students[0].AccountNumber != "0012" {
		t.Fatalf("result = %+v, %v", result, err)
	}
}
