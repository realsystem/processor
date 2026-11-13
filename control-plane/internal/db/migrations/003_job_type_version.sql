-- Typed job contracts (Autonomy J3): the version of the job type a job was submitted under.
-- NULL means the grandfathered, untyped contract ("0").
ALTER TABLE jobs ADD COLUMN job_type_version TEXT;
