-- Add fan_out_partial_failure to inbox_items kind constraint.
DO $$ BEGIN
  ALTER TABLE inbox_items DROP CONSTRAINT IF EXISTS inbox_items_kind_check;
  ALTER TABLE inbox_items ADD CONSTRAINT inbox_items_kind_check
    CHECK (kind IN ('awaiting_input','output_ready','notify','request_input','fan_out_partial_failure'));
EXCEPTION WHEN others THEN NULL;
END $$;
