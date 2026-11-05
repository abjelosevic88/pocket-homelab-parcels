-- 0.1.0 read Pocket Mail's message answer wrongly: every email found had no
-- sender, subject or message id. Forget those finds and the steps they made,
-- and let the next scan read the same emails again.
DELETE FROM mail_finds WHERE mail_message_id = 0;
DELETE FROM parcel_events WHERE source = 'mail' AND description LIKE 'Email from :%';
UPDATE parcels SET status_text = '' WHERE status_text LIKE 'Email from :%';
DELETE FROM cache WHERE key LIKE 'mailscan:%';
