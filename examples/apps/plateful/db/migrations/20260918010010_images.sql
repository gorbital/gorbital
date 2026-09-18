-- Images: one row per file a restaurant's staff upload through
-- gorbital.dev/modules/storage. The bytes live in the storage bucket; this
-- table is the record of what was asked for, where it went and whether it
-- ever arrived. The two are separate on purpose: the client uploads to a
-- signed URL without touching the app, so the row exists before the object
-- does, and status says which of the two states it is in.

-- +goose Up
-- docs:start images-table
CREATE TABLE images (
    id         text        PRIMARY KEY,
    -- The organisation whose staff own the image. It is also the first
    -- segment of storage_key, so one restaurant's key can never name
    -- another's object.
    org_id     text        NOT NULL,
    -- The member who asked for the upload, for display and audit.
    created_by text        NOT NULL,
    -- What the image is for. The restaurants and menu items modules read
    -- these rows by ID; the purpose says which of them may point at it.
    purpose    text        NOT NULL
                           CHECK (purpose IN ('restaurant_cover', 'menu_item_photo')),
    -- The object's key in the storage bucket, derived from org_id and id
    -- rather than from anything the client sends. UNIQUE because two rows
    -- naming one object would delete each other's bytes.
    storage_key text       NOT NULL UNIQUE,
    -- The image's media type. Asked for when the upload is requested, then
    -- replaced at confirmation with what the store reports for the object
    -- that actually arrived.
    content_type text      NOT NULL CHECK (char_length(content_type) <= 100),
    -- How large the object is, from the store's own Stat. Zero until the
    -- upload is confirmed, because nothing has been measured yet.
    size_bytes bigint      NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    -- pending from the moment the signed PUT is handed out; ready once the
    -- object has been found, measured and accepted. A pending row may have
    -- no object behind it at all: the client may never have uploaded.
    status     text        NOT NULL DEFAULT 'pending'
                           CHECK (status IN ('pending', 'ready')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    -- Other organisation tables reference (org_id, id), so a row can only
    -- point at an image of its own organisation.
    UNIQUE (org_id, id)
);
-- docs:end images-table

-- The organisation's own library, newest first, narrowed by purpose: the
-- picker a restaurant's staff use when they choose a cover or a photo.
CREATE INDEX images_org_purpose_created ON images (org_id, purpose, created_at, id);

-- Purging an organisation deletes its images. orgs is the organisations
-- module's table (orgshttp), which the app's migrations run with; an app
-- migrated without it, such as another module's test app, gets the table
-- without the foreign key. The objects in the bucket are not reached by a
-- foreign key and outlive the rows; a housekeeping job would sweep them.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('orgs') IS NOT NULL THEN
        ALTER TABLE images ADD CONSTRAINT images_org_id_fkey
            FOREIGN KEY (org_id) REFERENCES orgs (id) ON DELETE CASCADE;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE images;
