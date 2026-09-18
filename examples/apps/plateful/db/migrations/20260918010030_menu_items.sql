-- Menu items: every dish a restaurant sells, one row each. A menu reads as
-- sections of items, but a section is not a thing a restaurant owns
-- separately from its dishes: it has no settings, no lifecycle and nothing
-- references it. Carrying the section name on the item and grouping on the
-- way out keeps a rename, a reorder and a move between sections a single
-- UPDATE, and spares the app a second table, a join and the empty-section
-- case. A section that holds no items simply stops existing.

-- +goose Up
-- docs:start menu-items-table
CREATE TABLE menu_items (
    id         text        PRIMARY KEY,
    -- The organisation whose staff sell this dish. Every read is filtered by
    -- it, and (org_id, id) below lets other tables reference an item of
    -- their own organisation only.
    org_id     text        NOT NULL,
    -- The member who added the dish, for display and audit.
    created_by text        NOT NULL,
    -- The part of the menu the dish is printed under ("Starters"). The name
    -- is the section: there is no sections table, because a section has no
    -- life of its own once its last dish is gone.
    section    text        NOT NULL CHECK (char_length(section) BETWEEN 1 AND 60),
    -- Where the dish sits inside its section, smallest first. Ties are
    -- broken by ID, so a menu always reads in the same order.
    position   integer     NOT NULL DEFAULT 0,
    name       text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    description text       NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
    -- The price in integer minor units: 950 is £9.50, never 9.5. Money is
    -- never a float here. A float cannot represent 0.10 exactly — the
    -- nearest double is a hair above it — so a bill of ten 10p sides adds up
    -- to something that is not £1.00, and the error grows with every line and
    -- every rounding. An integer count of pennies adds up exactly, and
    -- bigint leaves room for currencies whose minor unit is small.
    price_minor bigint     NOT NULL CHECK (price_minor >= 0),
    -- The ISO 4217 code the price is in, three letters.
    currency   text        NOT NULL DEFAULT 'GBP' CHECK (char_length(currency) = 3),
    -- The switch a kitchen flicks when it runs out for the evening. An
    -- unavailable dish stays on the menu for staff and disappears from the
    -- customers' one; deleting is for a dish that should never have existed.
    available  boolean     NOT NULL DEFAULT true,
    -- NULL means unlimited: the kitchen cooks it to order and never counts.
    -- A number is limited stock, which the orders module decrements in SQL
    -- inside the transaction that places the order.
    stock      integer     CHECK (stock IS NULL OR stock >= 0),
    -- The dietary flags the domain allows, stored deduplicated and sorted so
    -- two items with the same flags hold the same array.
    dietary    text[]      NOT NULL DEFAULT '{}',
    -- The images module's ID of the dish's photo, empty when it has none.
    photo_image_id text    NOT NULL DEFAULT '',
    -- Increases with every update; an update must send the version it read.
    version    bigint      NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    -- Other organisation tables reference (org_id, id), so a row can only
    -- point at an item of its own organisation.
    UNIQUE (org_id, id)
);
-- docs:end menu-items-table

-- One restaurant never sells two dishes of the same name, ignoring case:
-- a customer could not tell them apart, and an order line names the dish.
CREATE UNIQUE INDEX menu_items_org_name ON menu_items (org_id, lower(name));

-- The menu read, staff's and customers' alike: one organisation's items in
-- section, then position, then ID order.
CREATE INDEX menu_items_org_section_position ON menu_items (org_id, section, position, id);

-- Purging an organisation deletes its menu. orgs is the organisations
-- module's table (orgshttp), which the app's migrations run with; an app
-- migrated without it, such as another module's test app, gets the table
-- without the foreign key.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('orgs') IS NOT NULL THEN
        ALTER TABLE menu_items ADD CONSTRAINT menu_items_org_id_fkey
            FOREIGN KEY (org_id) REFERENCES orgs (id) ON DELETE CASCADE;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE menu_items;
