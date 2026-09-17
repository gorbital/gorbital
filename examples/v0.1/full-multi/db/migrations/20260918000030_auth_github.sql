-- GitHub sign-in (ADR-0059): github joins the providers of identities, web
-- sign-ins and nonces, and a web sign-in can link a provider to the
-- signed-in account that started it instead of signing in.

-- +goose Up
ALTER TABLE auth_identities
    DROP CONSTRAINT auth_identities_provider_check,
    ADD CONSTRAINT auth_identities_provider_check CHECK (provider IN ('google', 'apple', 'github'));

ALTER TABLE auth_social_nonces
    DROP CONSTRAINT auth_social_nonces_provider_check,
    ADD CONSTRAINT auth_social_nonces_provider_check CHECK (provider IN ('google', 'apple', 'github'));

ALTER TABLE auth_oauth_states
    DROP CONSTRAINT auth_oauth_states_provider_check,
    ADD CONSTRAINT auth_oauth_states_provider_check CHECK (provider IN ('google', 'apple', 'github')),
    -- A link started by a signed-in user: the account and the session that
    -- started it, which must still be active when the provider returns.
    ADD COLUMN link_user_id    text REFERENCES auth_users (id) ON DELETE CASCADE,
    ADD COLUMN link_session_id text,
    ADD CONSTRAINT auth_oauth_states_link_check CHECK ((link_user_id IS NULL) = (link_session_id IS NULL));
