-- name: ListCompanies :many
SELECT * FROM companies ORDER BY name;

-- name: GetCompanyByName :one
SELECT * FROM companies WHERE name = ? COLLATE NOCASE;

-- name: CreateCompany :one
INSERT INTO companies (name, created_at) VALUES (?, ?) RETURNING id;

-- name: DeleteCompany :exec
DELETE FROM companies WHERE id = ?;

-- name: CountProjectsForCompany :one
SELECT COUNT(*) FROM projects WHERE company_id = ?;
