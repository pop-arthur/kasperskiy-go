-- name: GetThreat :one
SELECT md5, name, description, created_at, updated_at
FROM public.threats WHERE md5 = $1;

-- name: CreateThreat :one
INSERT INTO public.threats (md5, name, description)
VALUES ($1, $2, $3)
RETURNING md5, name, description, created_at, updated_at;

-- name: UpdateThreat :one
UPDATE public.threats
SET name = $2, description = $3, updated_at = CURRENT_TIMESTAMP
WHERE md5 = $1
RETURNING md5, name, description, created_at, updated_at;
