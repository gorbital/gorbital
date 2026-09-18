-- Customers: who ordered, and where they want food taken. One row per
-- account, created when the account is, by the app's registration hook
-- (cmd/api/signin.go).
--
-- It has no org_id, and could not: a customer is a member of no
-- organisation. It lives in the orders module because the delivery address
-- is the thing an order needs, and a module owns the data its operations
-- read.

-- +goose Up
-- docs:start customers-table
CREATE TABLE customers (
    -- The sign-in account. Deleting an account takes the profile with it.
    user_id      text        PRIMARY KEY,
    display_name text        NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 80),
    -- Where they usually want an order taken; '' until they say. An order
    -- may always name a different address.
    address      text        NOT NULL DEFAULT '' CHECK (char_length(address) <= 200),
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL
);
-- docs:end customers-table

-- auth_users is sign-in's table (authhttp), which the app's migrations run
-- with; an app migrated without it, such as another module's test app, gets
-- the table without the foreign key.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('auth_users') IS NOT NULL THEN
        ALTER TABLE customers ADD CONSTRAINT customers_user_id_fkey
            FOREIGN KEY (user_id) REFERENCES auth_users (id) ON DELETE CASCADE;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE customers;
