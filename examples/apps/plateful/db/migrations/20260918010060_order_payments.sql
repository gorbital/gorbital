-- Order payments: what a customer owes for one order, and the provider's
-- events about it. Two tables rather than one, because they answer two
-- different questions: order_payments is the current state of the money, and
-- payment_events is the record of which deliveries have already been applied
-- to it.
--
-- The payment is modelled on a generic provider: the app creates a pending
-- payment with the reference the provider handed back, and the provider says
-- later, by webhook, whether it was authorised, captured or failed. Nothing
-- in these tables names a particular provider.

-- +goose Up
-- docs:start order-payments-table
CREATE TABLE order_payments (
    id          text        PRIMARY KEY,
    -- The restaurant's organisation, copied from the order. The customer is
    -- a member of no organisation at all, so this column is the restaurant's
    -- side of the row, not the payer's.
    org_id      text        NOT NULL,
    -- One payment per order, which is what UNIQUE says: a customer who pays
    -- the same order twice must get the payment they already have back, not
    -- a second one. It is also what the orders module's rule reads.
    order_id    text        NOT NULL UNIQUE,
    -- The account that pays. Not a foreign key: payments outlive accounts,
    -- and the rule about which payments a customer may see is in the use
    -- cases (internal/modules/payments/usecase/pay_order.go).
    customer_id text        NOT NULL,
    -- Money in integer minor units (pence), never a float: 0.1 has no exact
    -- binary representation, and what a customer is charged must equal the
    -- order's total to the penny. The currency is stored beside it so the
    -- number is never ambiguous.
    amount_minor bigint     NOT NULL CHECK (amount_minor >= 0),
    currency    text        NOT NULL DEFAULT 'GBP' CHECK (char_length(currency) = 3),
    -- The status machine, whose transitions are in the domain
    -- (internal/modules/payments/domain/payment.go): pending to authorised
    -- to captured, pending to failed, and authorised or captured to
    -- refunded. The CHECK lists the statuses; it can't express the arrows
    -- between them, which is why the domain still has to.
    status      text        NOT NULL DEFAULT 'pending'
                            CHECK (status IN ('pending', 'authorised', 'captured', 'refunded', 'failed')),
    -- The provider's own identifier for this payment, returned when it was
    -- created and quoted in every event about it. Empty only for a payment
    -- the provider never acknowledged.
    provider_ref text       NOT NULL DEFAULT '' CHECK (char_length(provider_ref) <= 200),
    -- Why the provider refused it, shown to the customer. Empty unless the
    -- status is failed.
    failure_reason text     NOT NULL DEFAULT '' CHECK (char_length(failure_reason) <= 200),
    -- Increases with every change; an update names the version it read, so a
    -- webhook and a refund can't overwrite each other.
    version     bigint      NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL,
    -- Other organisation tables reference (org_id, id), so a row can only
    -- point at a payment of its own organisation.
    UNIQUE (org_id, id),
    -- The pair, not just order_id: a payment can only belong to an order of
    -- the organisation it says it does.
    FOREIGN KEY (org_id, order_id) REFERENCES orders (org_id, id) ON DELETE CASCADE
);
-- docs:end order-payments-table

-- A customer's own payments, newest first, across every restaurant.
CREATE INDEX order_payments_customer ON order_payments (customer_id, created_at DESC, id DESC);
-- The restaurant's view of money it is owed, and the platform's reconciliation.
CREATE INDEX order_payments_org_status ON order_payments (org_id, status, created_at DESC, id DESC);

-- docs:start payment-events-table
-- The provider's deliveries, one row each. The provider retries a delivery
-- it is not sure arrived, so the same event arrives more than once with a
-- fresh signature every time: the signature proves who sent it, and proves
-- nothing about whether it has been applied. The event ID is what stops it
-- being applied twice, which is why it is the primary key — the insert and
-- the status change happen in one transaction, so a redelivery loses the
-- race against the first and changes nothing.
--
-- payment_id is not a foreign key on purpose: purging an organisation takes
-- its orders and their payments with it, and the record of what the provider
-- sent should outlive them rather than block the purge.
CREATE TABLE payment_events (
    event_id    text        PRIMARY KEY,
    payment_id  text        NOT NULL,
    -- The provider's event type, as delivered, such as payment.authorised.
    kind        text        NOT NULL,
    received_at timestamptz NOT NULL
);
-- docs:end payment-events-table

-- What the provider has said about one payment, in order: the audit trail a
-- reconciliation reads when a status looks wrong.
CREATE INDEX payment_events_payment ON payment_events (payment_id, received_at);

-- Purging an organisation deletes its payments. orgs is the organisations
-- module's table (orgshttp), which the app's migrations run with; an app
-- migrated without it, such as another module's test app, gets the table
-- without the foreign key.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('orgs') IS NOT NULL THEN
        ALTER TABLE order_payments ADD CONSTRAINT order_payments_org_id_fkey
            FOREIGN KEY (org_id) REFERENCES orgs (id) ON DELETE CASCADE;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE payment_events;
DROP TABLE order_payments;
