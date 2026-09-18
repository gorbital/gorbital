-- Reviews: a diner rates an order that was delivered to them. This is the
-- third shape of ownership in Plateful, and the awkward one. The row belongs
-- to the restaurant's organisation, the person who wrote it is a member of no
-- organisation at all, and everybody on the internet may read it — a diner
-- choosing where to eat has not signed in yet.
--
-- The second table, restaurant_ratings, is the running total the public list
-- shows. It lives here rather than on the restaurant's row in orgs because it
-- is derived from these rows: the module that owns the reviews owns what is
-- computed from them, and the restaurants module never has to know a review
-- exists.

-- +goose Up
-- docs:start reviews-table
CREATE TABLE reviews (
    id            text     PRIMARY KEY,
    -- The restaurant, which is an organisation. The review is the tenant's
    -- data even though no member of the tenant wrote it, so it is purged
    -- with the organisation and joins like every other org-scoped row.
    org_id        text     NOT NULL,
    -- The order being reviewed. UNIQUE is the rule "one review per order":
    -- the database enforces it, so two requests that race still leave one
    -- review and the loser gets review_exists.
    order_id      text     NOT NULL UNIQUE,
    -- The account that placed the order. Not a foreign key, like
    -- orders.customer_id: reviews outlive accounts. It never leaves the
    -- server — the public list returns the rating and the comment, never who
    -- wrote them.
    customer_id   text     NOT NULL,
    rating        smallint NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment       text     NOT NULL DEFAULT '' CHECK (char_length(comment) <= 2000),
    -- Platform staff hide an abusive review. The row stays: hiding is a
    -- moderation decision that has to be reversible and auditable, and the
    -- customer keeps what they wrote.
    hidden        boolean  NOT NULL DEFAULT false,
    hidden_reason text     NOT NULL DEFAULT '' CHECK (char_length(hidden_reason) <= 500),
    version       bigint   NOT NULL DEFAULT 1,
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    -- Other organisation tables reference (org_id, id), so a row can only
    -- point at a review of its own organisation.
    UNIQUE (org_id, id),
    -- The order is read by ID with SQL (the orders module owns the table),
    -- but the link is still a real constraint: a review can only name an
    -- order of its own organisation, and it goes when the order goes.
    FOREIGN KEY (org_id, order_id) REFERENCES orders (org_id, id) ON DELETE CASCADE
);
-- docs:end reviews-table

-- docs:start restaurant-ratings-table
-- The running total behind the public list: how many reviews a restaurant
-- has and what they add up to. The average is not stored — it is rating_sum
-- over review_count, computed when it is read, so it can never drift from
-- the rows it comes from and never loses a penny of precision to a float.
--
-- It is written in the same transaction as the review it counts (see
-- repository/upsert_rating.go), so the number a diner sees is never stale.
-- A busier platform would trade that for a smaller transaction and
-- recompute it in a job.
-- The columns carry no CHECK (review_count >= 0). The upsert that maintains
-- them inserts a delta, which is negative when a review is hidden, and
-- PostgreSQL applies a column's CHECK to the row it is about to insert
-- before ON CONFLICT redirects it to the update — so such a constraint would
-- refuse every moderation of a restaurant's last review. The invariant is
-- kept by the use cases instead: a review contributes exactly once, and is
-- taken back out exactly once (domain.Review.Contribution).
CREATE TABLE restaurant_ratings (
    -- The restaurant: its organisation's id.
    org_id        text    PRIMARY KEY,
    review_count  integer NOT NULL DEFAULT 0,
    rating_sum    bigint  NOT NULL DEFAULT 0,
    updated_at    timestamptz NOT NULL
);
-- docs:end restaurant-ratings-table

-- The public list: a restaurant's visible reviews, newest first. Partial, so
-- the scan skips hidden rows however many of them moderation accumulates.
CREATE INDEX reviews_restaurant_created ON reviews (org_id, created_at DESC, id DESC)
    WHERE NOT hidden;

-- Purging an organisation deletes its reviews. orgs is the organisations
-- module's table (orgshttp), which the app's migrations run with; an app
-- migrated without it, such as another module's test app, gets the table
-- without the foreign key.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('orgs') IS NOT NULL THEN
        ALTER TABLE reviews ADD CONSTRAINT reviews_org_id_fkey
            FOREIGN KEY (org_id) REFERENCES orgs (id) ON DELETE CASCADE;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE restaurant_ratings;
DROP TABLE reviews;
