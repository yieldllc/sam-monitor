-- 0004_seed_primes.sql — seed the 13 primes from research/03-opportunity-research.md §1.
-- Uses NOT EXISTS guard on `prime` so re-running is a no-op (prime_poc has no UNIQUE
-- constraint on prime in 0001_init.sql).

INSERT INTO prime_poc (prime, contact_url, programs, notes)
SELECT v.prime, v.contact_url, v.programs, v.notes
FROM (VALUES
  ('Raft LLC',              'https://teamraft.com/contact-us/',
     ARRAY['Platform One','Kessel Run','Space CAMP'],
     '8(a) WOSB; small-business-to-small-business teaming realistic'),
  ('BridgePhase',           'https://bridgephase.com/contact-us/',
     ARRAY['Unified Platform'],
     '$48M Aug 2025 → Feb 2030; ramping through 2026'),
  ('OMNI Federal',          'https://omnifederal.com/contact-us/',
     ARRAY['Kessel Run EnDOR','Platform One','FORGE C2'],
     'SBA Mentor-Protégé JV available'),
  ('Sigma Defense',         'https://sigmadefense.com/contact-us/',
     ARRAY['Black Pearl'],
     'Navy/USMC software factory'),
  ('Booz Allen',            'https://doingbusiness.bah.com/',
     ARRAY['Platform One','Big Bang','Iron Bank'],
     'Subbed $1B+ FY20, >66% to SB'),
  ('SAIC',                  'https://www.saic.com/who-we-are/suppliers-and-small-business',
     ARRAY['Cloud One','Kessel Run AOC'],
     'Structured SBLO program'),
  ('Leidos',                'https://www.leidos.com/suppliers/small-business-relationships',
     ARRAY['Kessel Run C2IMERA','USAF cloud arch'],
     'Multiple SBLOs by sector'),
  ('GDIT',                  'https://www.gdit.com/about/suppliers/small-business/',
     ARRAY['NIWC Pacific'],
     'TS/SCI K8s work in San Diego'),
  ('Atlas Technologies',    'https://atlastechnologies.com/contact/',
     ARRAY['NIWC Atlantic'],
     'VOSB; $249M IDIQ'),
  ('Core4ce',               'https://www.core4ce.com/contact/',
     ARRAY['NIWC Atlantic CSSP'],
     '$90M 5yr CSSP OTA'),
  ('Defense Unicorns',      'https://defenseunicorns.com/contact-us/',
     ARRAY['DIA WAEDS','UDS'],
     'VOSB peer; air-gap K8s'),
  ('Second Front Systems',  'https://www.secondfront.com/contact/',
     ARRAY['Game Warden'],
     'Carahsoft reseller; AWS JWCC'),
  ('SealingTech (Parsons)', 'https://sealingtech.com/contact/',
     ARRAY['DREN','USCYBERCOM JCHK'],
     '$500M USCYBERCOM ceiling')
) AS v(prime, contact_url, programs, notes)
WHERE NOT EXISTS (SELECT 1 FROM prime_poc p WHERE p.prime = v.prime);
