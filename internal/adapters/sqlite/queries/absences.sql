-- name: CreateAbsence :one
INSERT INTO absences (date, type, fraction, note) VALUES (?, ?, ?, ?) RETURNING id;

-- name: DeleteAbsence :exec
DELETE FROM absences WHERE id = ?;

-- name: ListAbsencesBetween :many
SELECT * FROM absences WHERE date BETWEEN @from_date AND @to_date ORDER BY date;
