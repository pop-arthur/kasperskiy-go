CREATE TABLE public.threats (
    md5 text COLLATE "C" NOT NULL,
    name text NOT NULL,
    description text,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT threats_pkey PRIMARY KEY (md5),
    CONSTRAINT threats_md5_format_check CHECK (md5 ~ '^[a-f0-9]{32}$')
);
