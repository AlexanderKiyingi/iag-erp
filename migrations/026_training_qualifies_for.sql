-- 026: what a training course qualifies someone to do
--
-- Quality control asks HR one question before it accepts a result: is the
-- person who recorded it trained for the method they used? Courses already
-- record completion and expiry (011); they did not say what they qualify for.
--
--   qualifies_for  quality-control lab method ids (LM-…) a completed,
--                  unexpired enrolment in this course qualifies its holder to
--                  run. Empty for courses that qualify for nothing in the lab.
--
-- Ship this BEFORE the code that reads it: production runs migrations out of
-- band (AUTO_MIGRATE=false), and the course queries select the column.

ALTER TABLE erp_training_courses
    ADD COLUMN IF NOT EXISTS qualifies_for TEXT[] NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS erp_training_courses_qualifies_for_idx
    ON erp_training_courses USING GIN (qualifies_for);
