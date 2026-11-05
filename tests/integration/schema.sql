BEGIN;

DO $$
DECLARE
    record public.threats%ROWTYPE;
BEGIN
    INSERT INTO public.threats (md5, name)
    VALUES ('d41d8cd98f00b204e9800998ecf8427e', '')
    RETURNING * INTO record;
    IF record.description IS NOT NULL OR record.created_at IS NULL
       OR record.updated_at IS NULL OR record.created_at <> record.updated_at THEN
        RAISE EXCEPTION 'Invalid NULL or timestamp defaults';
    END IF;

    INSERT INTO public.threats (md5, name, description)
    VALUES ('00000000000000000000000000000000', '', '');
    IF NOT EXISTS (SELECT 1 FROM public.threats
                   WHERE md5 = '00000000000000000000000000000000'
                     AND description = '') THEN
        RAISE EXCEPTION 'Empty description was not preserved';
    END IF;

    BEGIN
        INSERT INTO public.threats (md5, name)
        VALUES ('d41d8cd98f00b204e9800998ecf8427e', 'duplicate');
        RAISE EXCEPTION 'Duplicate MD5 was accepted';
    EXCEPTION WHEN unique_violation THEN NULL;
    END;

    BEGIN
        INSERT INTO public.threats (md5, name) VALUES (NULL, 'null md5');
        RAISE EXCEPTION 'NULL MD5 was accepted';
    EXCEPTION WHEN not_null_violation THEN NULL;
    END;

    BEGIN
        INSERT INTO public.threats (md5, name)
        VALUES ('11111111111111111111111111111111', NULL);
        RAISE EXCEPTION 'NULL name was accepted';
    EXCEPTION WHEN not_null_violation THEN NULL;
    END;
END $$;

DO $$
DECLARE
    bad_md5 text;
BEGIN
    FOREACH bad_md5 IN ARRAY ARRAY[
        '', 'short', repeat('g', 32), repeat('0', 31), repeat('0', 33),
        'D41D8CD98F00B204E9800998ECF8427E',
        'd41d8cd98f00b204e9800998ecf8427e' || chr(10),
        'd41d8cd98f00b204e9800998ecf8427e' || ' '
    ] LOOP
        BEGIN
            INSERT INTO public.threats (md5, name) VALUES (bad_md5, 'invalid');
            RAISE EXCEPTION 'Invalid MD5 was accepted';
        EXCEPTION WHEN check_violation THEN NULL;
        END;
    END LOOP;
END $$;

ROLLBACK;
