-- Platform is discovered during enrollment. Existing implicit Windows values
-- are converted to unknown so no platform receives an accidental preference.
UPDATE devices SET platform = 'unknown' WHERE platform = 'windows';
