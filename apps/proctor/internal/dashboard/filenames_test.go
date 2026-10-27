package dashboard

import (
	"strings"
	"testing"
)

func TestNormalizeFileName(t *testing.T) {
	for _, item := range []struct {
		name, account, want string
	}{
		{"José Ñúñez Martínez", "20231234", "Jose_Nunez_Martinez"},
		{"Ñúñez Martínez, José", "20231234", "Nunez_Martinez_Jose"},
		{"Ana   María", "1", "Ana_Maria"},
		{"  --Hello..  ", "1", "Hello"},
		{"Straße æ ØđŁœ", "1", "Strasse_aeOdLoe"},
		{"a/b:c'd", "1", "abcd"},
		{"", "20231234", "20231234"},
		{"   ", "20231234", "20231234"},
		{"_index", "1", "index"},
		{strings.Repeat("a", 150), "1", strings.Repeat("a", 100)},
		{"Ada", "", "Ada"},
	} {
		if got := normalizeFileName(item.name, item.account); got != item.want {
			t.Errorf("normalizeFileName(%q, %q) = %q, want %q", item.name, item.account, got, item.want)
		}
	}
}

func TestCollidingStudentNamesAllGetTheirAccount(t *testing.T) {
	students := []downloadStudent{
		{Account: "0001", Name: "José Núñez"},
		{Account: "0002", Name: "JOSE NUNEZ"},
		{Account: "0003", Name: "Someone Else"},
	}
	names := studentFileNames(students)
	if names["0001"] != "Jose_Nunez_0001.md" || names["0002"] != "JOSE_NUNEZ_0002.md" || names["0003"] != "Someone_Else.md" {
		t.Fatalf("names = %v", names)
	}
}

func TestDownloadFolderName(t *testing.T) {
	got := downloadFolderName("CCC403 Sección 1", "Examen I Parcial – Organización de Computadoras")
	want := "CCC403_Seccion_1-Examen_I_Parcial_Organizacion_de_Computadoras"
	if got != want {
		t.Fatalf("folder name = %q, want %q", got, want)
	}
	if got := downloadFolderName("", ""); got != "class-exam" {
		t.Fatalf("empty folder name = %q", got)
	}
}
