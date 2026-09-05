-- +goose Up
ALTER TABLE email_bridge.receipts
 ADD COLUMN report_source jsonb,
 ADD COLUMN report_source_digest text NOT NULL DEFAULT '',
 ADD COLUMN report_version bigint NOT NULL DEFAULT 0 CHECK (report_version>=0),
 ADD COLUMN report_pending boolean NOT NULL DEFAULT false,
 ADD COLUMN report_after timestamptz,
 ADD COLUMN report_completed boolean NOT NULL DEFAULT false,
 ADD CONSTRAINT report_source_bound CHECK (
   (report_version=0 AND report_source IS NULL AND report_source_digest='' AND NOT report_pending)
   OR (report_version>0 AND report_source_digest ~ '^[a-f0-9]{64}$' AND report_after IS NOT NULL
     AND (NOT report_pending OR report_source IS NOT NULL))),
 ADD CONSTRAINT report_source_size CHECK (report_source IS NULL OR
   (jsonb_typeof(report_source)='object' AND octet_length(report_source::text)<=8192));
CREATE INDEX pending_email_reports ON email_bridge.receipts(report_after,tenant_id,mailbox_id,message_id)
 WHERE report_pending;
