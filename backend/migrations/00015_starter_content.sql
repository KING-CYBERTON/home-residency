-- +goose Up
-- Starter content so a new environment is not empty: published FAQs for the public site, draft
-- website pages, and training courses. The induction is active, because every demo checklist asks
-- for "Homend induction training" and an applicant can only be enrolled on a running course. The
-- category courses start inactive, because every active course becomes an activation requirement
-- (activationBlockers); the training coordinator switches each one on when it is ready.
-- Category rows are matched by slug and are skipped where the category does not exist, and
-- nothing that already exists is overwritten, so edits made in the console are kept.

INSERT INTO faqs (category_id, question, answer, sort_order)
SELECT NULL, q, a, n FROM (VALUES
  (10, 'How do you check providers?',
   'Every provider applies online for a specific service. We verify their identity and documents with the issuing source, speak to two referees, independently confirm their Certificate of Good Conduct, and score a structured interview and a practical assessment. A manager who did not do the vetting approves them, and nobody works before finishing our training.'),
  (20, 'Do I deal with the provider directly?',
   'No. Every booking runs through our team. You tell us what you need, we choose the provider and record why, and messages go through us. Households and providers never see each other''s phone numbers.'),
  (30, 'How do I book?',
   'Choose the service, date, time and scope from the rate card on this site or in your account. Prices are fixed, so there is no haggling. We confirm the provider and send you the booking details by SMS.'),
  (40, 'How do I pay?',
   'You pay by M-Pesa against an invoice in your account. We pay providers on a published cycle, so you never need to handle cash.'),
  (50, 'How do I know the provider has arrived?',
   'The provider checks in on arrival with a code or their location, and the booking in your account shows it.'),
  (60, 'What if the work or the price needs to change?',
   'Any change to the work or the price is proposed in the app and only goes ahead once it is approved.'),
  (70, 'What happens if something goes wrong?',
   'Raise a complaint in your account; a low rating opens one automatically. Safety and loss complaints are escalated at once and the provider is paused while we investigate. We hear the provider''s side before deciding and tell you what we did about it.'),
  (80, 'Which areas do you cover?',
   'The areas we serve are listed on the Areas page. If yours is not there, send us an enquiry through the contact form and tell us where you are.'),
  (90, 'How do you look after my data?',
   'We handle personal data under Kenya''s Data Protection Act, 2019. Documents and sensitive details such as gate codes are encrypted, only staff whose role needs them can open them, and every access is logged. See our privacy notice for details.'),
  (100, 'How do I work with Homend Residence?',
   'Apply online from the Work with us page, choose the kind of work you do, upload your documents and give two referees. We will SMS you at each step of vetting and training.')
) AS v(n, q, a)
WHERE NOT EXISTS (SELECT 1 FROM faqs f WHERE f.question = v.q AND f.language = 'en');

INSERT INTO faqs (category_id, question, answer, sort_order)
SELECT c.id, v.q, v.a, v.n FROM (VALUES
  ('cleaning', 10, 'Do I need to supply cleaning products?',
   'Cleaners bring their own basic supplies. Tell us in the booking if you want them to use your own products or need anything special.'),
  ('cleaning', 20, 'What does a standard clean include?',
   'The scope for each cleaning service is listed on its page and in your booking, so you and the cleaner agree on the same list before the visit.'),
  ('live-in-house-help', 10, 'How does a live-in placement work?',
   'We place a vetted worker with you under a written contract with agreed duties, one full rest day a week, and NSSF and SHIF registration. Wages are paid on time through the platform.'),
  ('live-in-house-help', 20, 'Why do you need to check my ID for a live-in placement?',
   'A live-in placement puts a worker in your home full time, so we verify the household as well as the worker before we place anyone.'),
  ('plumbing', 10, 'Are your plumbers licensed?',
   'Yes. Trade providers are checked for the licence their work needs, as well as the identity, referee and conduct checks every provider goes through.'),
  ('electrical', 10, 'Are your electricians licensed?',
   'Yes. Trade providers are checked for the licence their work needs, as well as the identity, referee and conduct checks every provider goes through.'),
  ('caregiver', 10, 'What training do caregivers have?',
   'Caregivers go through enhanced vetting and must hold a current first-aid certificate before they are placed.')
) AS v(slug, n, q, a)
JOIN service_categories c ON c.slug = v.slug
WHERE NOT EXISTS (SELECT 1 FROM faqs f WHERE f.question = v.q AND f.language = 'en' AND f.category_id = c.id);

INSERT INTO content_pages (slug, title, meta_description, body_markdown, is_published) VALUES
  ('about', 'About Homend Residence', 'Homend Residence places vetted, trained and supervised home service providers with Nairobi households.',
   E'## Who we are\n\nHomend Residence is a managed home services company. We choose, check, train and supervise every provider, and every booking runs through our team.\n\n## What we do\n\n- One-off services such as cleaning, plumbing and electrical work\n- Day workers on fixed days\n- Live-in placements under a written contract\n\n## How we are different\n\nYou deal with us, and we deal with the provider. We stand behind every job and follow up every complaint.',
   false),
  ('estate-partnerships', 'Partnerships for estates and property managers', 'Vetted home services for residents of your estate or apartment block.',
   E'## For estates and property managers\n\nOffer your residents vetted, supervised home services with one point of contact for the whole estate.\n\n- Providers checked and trained before they enter the estate\n- Check-in on arrival, so the gate and the household know who is on site\n- Complaints handled by our team, with outcomes reported back\n\nSend us an enquiry through the contact form to talk about a partnership.',
   false)
ON CONFLICT (slug, language) DO NOTHING;

INSERT INTO training_courses (code, name, category_id, is_induction, pass_mark, is_active)
VALUES ('GEN-IND', 'Homend induction: conduct, safeguarding and the provider app', NULL, true, 70, true)
ON CONFLICT (code) DO NOTHING;

INSERT INTO training_courses (code, name, category_id, is_induction, pass_mark, is_active)
SELECT v.code, v.name, c.id, false, v.pass_mark, false FROM (VALUES
  ('cleaning', 'CLN-101', 'House cleaning standards', 70),
  ('live-in-house-help', 'LIV-101', 'Live-in house help: duties, household safety and rights', 70),
  ('day-worker', 'DAY-101', 'Day worker standards', 70),
  ('plumbing', 'PLB-101', 'Plumbing: safety and working in clients'' homes', 70),
  ('electrical', 'ELC-101', 'Electrical: safety and working in clients'' homes', 70),
  ('caregiver', 'CRG-101', 'Caregiving: dignity, safeguarding and first aid', 80)
) AS v(slug, code, name, pass_mark)
JOIN service_categories c ON c.slug = v.slug
ON CONFLICT (code) DO NOTHING;

-- +goose Down
DELETE FROM training_courses WHERE code IN ('GEN-IND', 'CLN-101', 'LIV-101', 'DAY-101', 'PLB-101', 'ELC-101', 'CRG-101')
  AND NOT EXISTS (SELECT 1 FROM training_enrolments e WHERE e.course_id = training_courses.id);
DELETE FROM content_pages WHERE slug IN ('about', 'estate-partnerships') AND language = 'en' AND NOT is_published;
-- The FAQs are left in place: they may have been edited in the console since.
