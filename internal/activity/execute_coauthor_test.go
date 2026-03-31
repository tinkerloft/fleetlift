package activity

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupUserGitIdentity(t *testing.T) {
	tests := []struct {
		name       string
		userID     string
		dbName     string
		dbEmail    string
		dbProvider string
		dbErr      error
		wantName   string
		wantEmail  string
		wantErrIs  error
	}{
		{
			name:      "not found returns ErrNoRows",
			userID:    "nonexistent",
			dbErr:     sql.ErrNoRows,
			wantErrIs: sql.ErrNoRows,
		},
		{
			name:       "user with email returns name and email",
			userID:     "user-1",
			dbName:     "Alice Smith",
			dbEmail:    "alice@example.com",
			dbProvider: "12345",
			wantName:   "Alice Smith",
			wantEmail:  "alice@example.com",
		},
		{
			name:       "user without email falls back to noreply address",
			userID:     "user-2",
			dbName:     "Bob Jones",
			dbProvider: "67890",
			wantName:   "Bob Jones",
			wantEmail:  "67890+noreply@users.noreply.github.com",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			q := mock.ExpectQuery(`SELECT name, COALESCE\(email, ''\), provider_id FROM users WHERE id = \$1`).
				WithArgs(tc.userID)
			if tc.dbErr != nil {
				q.WillReturnError(tc.dbErr)
			} else {
				q.WillReturnRows(sqlmock.NewRows([]string{"name", "coalesce", "provider_id"}).
					AddRow(tc.dbName, tc.dbEmail, tc.dbProvider))
			}

			gotName, gotEmail, gotErr := lookupUserGitIdentity(context.Background(), db, tc.userID)

			if tc.wantErrIs != nil {
				assert.ErrorIs(t, gotErr, tc.wantErrIs)
			} else {
				require.NoError(t, gotErr)
			}
			assert.Equal(t, tc.wantName, gotName)
			assert.Equal(t, tc.wantEmail, gotEmail)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
