-- Each producer owns its outbox and can publish independently of the gateway.
CREATE TABLE payment.event_outbox (LIKE public.event_outbox INCLUDING ALL);
CREATE TABLE notification.event_outbox (LIKE public.event_outbox INCLUDING ALL);
