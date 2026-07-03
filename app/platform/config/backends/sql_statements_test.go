package backends

import "testing"

func TestQuoteConfigSQLIdentifierAllowsOnlySafeNames(t *testing.T) {
	cases := []struct {
		name    string
		dialect string
		input   string
		want    string
		wantErr bool
	}{
		{name: "postgres letters digits underscore", dialect: "postgresql", input: "config_Store1", want: `"config_Store1"`},
		{name: "mysql letters digits underscore", dialect: "mysql", input: "config_Store1", want: "`config_Store1`"},
		{name: "empty", input: "", wantErr: true},
		{name: "dash", input: "config-store", wantErr: true},
		{name: "space", input: "config store", wantErr: true},
		{name: "quote", input: `config"store`, wantErr: true},
		{name: "semicolon", input: "config;drop", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := quoteConfigSQLIdentifier(tc.dialect, tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("quoteConfigSQLIdentifier(%q) error = nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("quoteConfigSQLIdentifier(%q) error = %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("quoteConfigSQLIdentifier(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
