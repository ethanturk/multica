-- Fork migrations 900/901 rebuilt this whitelist from a list that predates
-- upstream's later additions (muse, devin, zcode, droid, amp, ...). Because
-- they sort after those upstream migrations, they dropped them again. Resync
-- to upstream's current whitelist plus the fork's dirge. NOT VALID keeps
-- historical rows untouched, matching the upstream pattern.
ALTER TABLE runtime_profile
    DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check,
    ADD CONSTRAINT runtime_profile_protocol_family_check
    CHECK (protocol_family IN (
        'claude',
        'codebuddy',
        'codex',
        'copilot',
        'opencode',
        'codearts',
        'openclaw',
        'hermes',
        'pi',
        'cursor',
        'kimi',
        'reasonix',
        'dsh',
        'kiro',
        'antigravity',
        'qoder',
        'qoderclicn',
        'traecli',
        'deveco',
        'grok',
        'qwen',
        'qwenpaw',
        'mcode',
        'muse',
        'dim',
        'zeroclaw',
        'devin',
        'zcode',
        'droid',
        'amp',
        'dirge'
    )) NOT VALID;
