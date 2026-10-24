-- Demo catalogue for staging and local testing: service areas, categories, services, rate
-- card, vetting checklists and subscription plans. `api seed-demo` runs it in one
-- transaction. Every insert skips rows that already exist (by slug, code or "already has
-- one"), so running it twice is harmless and it never overwrites edits made in the console.
-- Prices are KES in minor units (cents): 350000 = KES 3,500.

BEGIN;

INSERT INTO service_areas (slug, name, county, is_active, centre_lat, centre_lng, radius_km) VALUES
  ('kilimani',    'Kilimani',    'Nairobi', true, -1.289800, 36.783300, 3.0),
  ('kileleshwa',  'Kileleshwa',  'Nairobi', true, -1.280500, 36.783900, 2.5),
  ('lavington',   'Lavington',   'Nairobi', true, -1.276900, 36.769400, 3.0),
  ('westlands',   'Westlands',   'Nairobi', true, -1.267600, 36.810800, 3.0),
  ('parklands',   'Parklands',   'Nairobi', true, -1.261900, 36.822800, 2.5),
  ('karen',       'Karen',       'Nairobi', true, -1.319000, 36.707300, 6.0),
  ('runda',       'Runda',       'Nairobi', true, -1.214000, 36.809600, 4.0),
  ('south-b',     'South B',     'Nairobi', true, -1.309800, 36.836400, 2.5),
  ('kitengela',   'Kitengela',   'Kajiado', false, -1.476700, 36.961900, 5.0)
ON CONFLICT (slug) DO NOTHING;

INSERT INTO service_categories (slug, name, name_sw, description, engagement, vetting_tier,
                                requires_client_id_verification, sop_markdown, is_active, sort_order) VALUES
  ('cleaning', 'House cleaning', 'Usafi wa nyumba',
   'One-off and deep cleaning by vetted cleaners who bring their own basic supplies.',
   'one_off', 'standard', false,
   E'## Standard clean\n- Dust all reachable surfaces\n- Sweep and mop floors\n- Clean kitchen counters, sink and hob\n- Clean and disinfect bathrooms\n- Empty bins\n- Make beds',
   true, 10),
  ('live-in-house-help', 'Live-in house help', 'Msaidizi wa nyumbani (kuishi ndani)',
   'Full-time live-in domestic worker placed with a contract, NSSF and SHIF registration.',
   'live_in', 'standard', true,
   E'## Live-in standard\n- Agreed duties written in the contract\n- One full rest day a week\n- Wages paid on time through the platform',
   true, 20),
  ('day-worker', 'Day worker', 'Mfanyakazi wa mchana',
   'Recurring day-based help on fixed days of the week.',
   'day_based', 'standard', false, NULL, true, 30),
  ('plumbing', 'Plumbing', 'Ufundi wa mabomba',
   'Leaks, blockages, taps, tanks and water heaters by licensed plumbers.',
   'one_off', 'trade', false, NULL, true, 40),
  ('electrical', 'Electrical', 'Ufundi wa umeme',
   'Sockets, lighting, wiring faults and appliance installation by licensed electricians.',
   'one_off', 'trade', false, NULL, true, 50),
  ('caregiver', 'Caregiver', 'Mlezi',
   'Elderly and special-needs care by first-aid trained caregivers.',
   'day_based', 'enhanced', true, NULL, true, 60),
  ('pest-control', 'Pest control', 'Kuangamiza wadudu',
   'Fumigation for cockroaches, bedbugs, termites and rodents.',
   'one_off', 'trade', false, NULL, false, 70)
ON CONFLICT (slug) DO NOTHING;

INSERT INTO services (category_id, slug, name, description, scope_items, pricing_unit,
                      min_quantity, max_quantity, default_duration_minutes, is_active, sort_order)
SELECT c.id, v.slug, v.name, v.description, v.scope::jsonb, v.unit::pricing_unit,
       v.min_q, v.max_q, v.minutes, true, v.sort
FROM (VALUES
  ('cleaning', 'standard-clean-1br', 'Standard clean, 1 bedroom', 'Regular clean of a 1-bedroom home.',
   '["Dusting","Floors","Kitchen","Bathroom","Bins"]', 'per_job', 1, 1, 180, 10),
  ('cleaning', 'standard-clean-3br', 'Standard clean, 3 bedrooms', 'Regular clean of a 3-bedroom home.',
   '["Dusting","Floors","Kitchen","Bathrooms","Bins","Beds"]', 'per_job', 1, 1, 300, 20),
  ('cleaning', 'deep-clean', 'Deep clean (per room)', 'Inside cupboards, windows, grout and appliances.',
   '["Inside cupboards","Windows","Grout","Oven and fridge"]', 'per_room', 1, 10, 90, 30),
  ('cleaning', 'move-out-clean', 'Move-in / move-out clean', 'Empty-house clean before or after a move.',
   '["Whole house","Windows","Cupboards","Bathrooms"]', 'per_job', 1, 1, 480, 40),
  ('live-in-house-help', 'live-in-general', 'Live-in house help', 'Cooking, cleaning, laundry and light childcare.',
   '["Cooking","Cleaning","Laundry","Light childcare"]', 'per_month', 1, 12, NULL, 10),
  ('live-in-house-help', 'live-in-nanny', 'Live-in nanny', 'Childcare-focused live-in placement.',
   '["Childcare","Child meals","Child laundry","School runs"]', 'per_month', 1, 12, NULL, 20),
  ('day-worker', 'day-worker-full-day', 'Day worker, full day', 'Cleaning and laundry, 8am to 5pm.',
   '["Cleaning","Laundry","Ironing"]', 'per_day', 1, 6, 540, 10),
  ('day-worker', 'laundry-ironing', 'Laundry and ironing', 'Hand or machine wash, dry and iron.',
   '["Washing","Drying","Ironing","Folding"]', 'per_hour', 2, 8, 120, 20),
  ('plumbing', 'plumbing-callout', 'Plumbing call-out and first hour', 'Diagnosis and the first hour of work.',
   '["Diagnosis","Up to 1 hour labour"]', 'per_job', 1, 1, 60, 10),
  ('plumbing', 'plumbing-hourly', 'Plumbing, extra hours', 'Labour after the first hour. Parts billed separately.',
   '["Labour"]', 'per_hour', 1, 8, 60, 20),
  ('electrical', 'electrical-callout', 'Electrical call-out and first hour', 'Fault finding and the first hour of work.',
   '["Fault finding","Up to 1 hour labour"]', 'per_job', 1, 1, 60, 10),
  ('electrical', 'appliance-install', 'Appliance installation', 'Cooker, water heater or washing machine hookup.',
   '["Install","Test","Safety check"]', 'per_job', 1, 3, 120, 20),
  ('caregiver', 'elderly-care-day', 'Elderly care, day shift', 'Companionship, meals, medication reminders, mobility help.',
   '["Companionship","Meals","Medication reminders","Mobility"]', 'per_day', 1, 7, 600, 10),
  ('pest-control', 'fumigation-house', 'House fumigation', 'Whole-house treatment for crawling insects.',
   '["Inspection","Treatment","Aftercare advice"]', 'per_job', 1, 1, 180, 10)
) AS v(cat, slug, name, description, scope, unit, min_q, max_q, minutes, sort)
JOIN service_categories c ON c.slug = v.cat
ON CONFLICT (category_id, slug) DO NOTHING;

-- One nationwide price per service, plus a Karen and Runda premium for the 3-bedroom clean.
INSERT INTO rate_card_entries (service_id, service_area_id, client_price_minor, provider_pay_minor, placement_fee_minor)
SELECT s.id, a.id, v.client, v.provider, v.placement
FROM (VALUES
  ('cleaning', 'standard-clean-1br',   NULL,      200000,  140000,      0),
  ('cleaning', 'standard-clean-3br',   NULL,      350000,  245000,      0),
  ('cleaning', 'standard-clean-3br',   'karen',   420000,  290000,      0),
  ('cleaning', 'standard-clean-3br',   'runda',   420000,  290000,      0),
  ('cleaning', 'deep-clean',           NULL,      150000,  100000,      0),
  ('cleaning', 'move-out-clean',       NULL,      900000,  600000,      0),
  ('live-in-house-help', 'live-in-general', NULL, 2000000, 1700000, 800000),
  ('live-in-house-help', 'live-in-nanny',   NULL, 2300000, 1950000, 1000000),
  ('day-worker', 'day-worker-full-day', NULL,     150000,  110000,  200000),
  ('day-worker', 'laundry-ironing',     NULL,      40000,   28000,      0),
  ('plumbing', 'plumbing-callout',      NULL,     250000,  175000,      0),
  ('plumbing', 'plumbing-hourly',       NULL,     120000,   85000,      0),
  ('electrical', 'electrical-callout',  NULL,     250000,  175000,      0),
  ('electrical', 'appliance-install',   NULL,     300000,  210000,      0),
  ('caregiver', 'elderly-care-day',     NULL,     250000,  180000,  300000),
  ('pest-control', 'fumigation-house',  NULL,     600000,  400000,      0)
) AS v(cat, svc, area, client, provider, placement)
JOIN service_categories c ON c.slug = v.cat
JOIN services s ON s.category_id = c.id AND s.slug = v.svc
LEFT JOIN service_areas a ON a.slug = v.area
WHERE NOT EXISTS (
  SELECT 1 FROM rate_card_entries r
  WHERE r.service_id = s.id AND r.service_area_id IS NOT DISTINCT FROM a.id
);

-- Vetting checklist v1 for each category that has none yet.
INSERT INTO checklist_templates (category_id, version, is_current, approved_at)
SELECT c.id, 1, true, now()
FROM service_categories c
WHERE c.slug IN ('cleaning', 'live-in-house-help', 'day-worker', 'plumbing', 'electrical', 'caregiver', 'pest-control')
  AND NOT EXISTS (SELECT 1 FROM checklist_templates t WHERE t.category_id = c.id);

-- Items go only into a v1 template that is still empty, so console edits are kept.
INSERT INTO checklist_template_items (template_id, kind, document_type, label, is_mandatory, min_count, validity_months, sort_order)
SELECT t.id, i.kind::checklist_item_kind, i.doc::document_type, i.label, i.mandatory, i.min_count, i.validity, i.sort
FROM service_categories c
JOIN checklist_templates t ON t.category_id = c.id AND t.version = 1
JOIN (VALUES
  -- Every tier
  ('all',      'document',  'national_id',              'National ID (both sides)',              true,  1, NULL::int, 10),
  ('all',      'document',  'passport_photo',           'Passport photo',                        true,  1, NULL,      20),
  ('all',      'document',  'kra_pin',                  'KRA PIN certificate',                   true,  1, NULL,      30),
  ('all',      'clearance', NULL,                       'Certificate of Good Conduct (DCI)',     true,  1, 12,        40),
  ('all',      'interview', NULL,                       'In-person interview',                   true,  1, NULL,      60),
  ('all',      'training',  NULL,                       'Homend induction training',             true,  1, NULL,      90),
  -- Standard: two referees
  ('standard', 'referee',   NULL,                       'Referees (previous employers)',         true,  2, NULL,      50),
  -- Trade: two referees, trade papers and a practical test
  ('trade',    'referee',   NULL,                       'Referees (previous clients)',           true,  2, NULL,      50),
  ('trade',    'document',  'trade_certificate',        'Trade certificate (NITA or TVET)',      true,  1, NULL,      70),
  ('trade',    'trade_licence', NULL,                   'Valid trade licence (EPRA or NCA)',     false, 1, 12,        75),
  ('trade',    'practical_assessment', NULL,            'Practical skills test',                 true,  1, NULL,      80),
  -- Enhanced (caregiving): three referees, first aid and a medical
  ('enhanced', 'referee',   NULL,                       'Referees (one from a care setting)',    true,  3, NULL,      50),
  ('enhanced', 'document',  'first_aid_certificate',    'First aid certificate',                 true,  1, 24,        70),
  ('enhanced', 'medical',   NULL,                       'Medical fitness check',                 true,  1, 12,        75),
  ('enhanced', 'practical_assessment', NULL,            'Care skills assessment',                true,  1, NULL,      80)
) AS i(tier, kind, doc, label, mandatory, min_count, validity, sort)
  ON i.tier = 'all' OR i.tier = c.vetting_tier::text
WHERE c.slug IN ('cleaning', 'live-in-house-help', 'day-worker', 'plumbing', 'electrical', 'caregiver', 'pest-control')
  AND NOT EXISTS (SELECT 1 FROM checklist_template_items x WHERE x.template_id = t.id);

INSERT INTO subscription_plans (code, name, description, price_minor, billing_period, benefits, is_active) VALUES
  ('basic-monthly', 'Homend Basic', 'Priority booking and 5% off every one-off job.',
   99900, 'monthly', '{"discount_pct":5,"priority_booking":true}', true),
  ('family-quarterly', 'Homend Family', '10% off, a 30-day replacement guarantee and one free deep clean room a quarter.',
   270000, 'quarterly', '{"discount_pct":10,"replacement_guarantee_days":30,"free_deep_clean_rooms":1}', true),
  ('premium-annual', 'Homend Premium', '15% off, a 90-day replacement guarantee and a dedicated account manager.',
   999900, 'annual', '{"discount_pct":15,"replacement_guarantee_days":90,"account_manager":true}', true)
ON CONFLICT (code) DO NOTHING;

COMMIT;
