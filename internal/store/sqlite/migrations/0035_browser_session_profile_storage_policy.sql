ALTER TABLE browser_session_profiles
ADD COLUMN profile_storage_policy TEXT NOT NULL DEFAULT 'encrypted_required';
