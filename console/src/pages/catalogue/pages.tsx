import type { Schemas } from "@/api/client";
import { EngagementType, Language, NotificationChannel, PricingUnit, VettingTier } from "@/api/enums.gen";
import { Badge } from "@/components/ui/badge";
import { useAreaName, useAreaOptions, useCategories, useCategoryName, useCategoryOptions } from "@/components/lookups";
import { StatusBadge } from "@/components/status-badge";
import { dateTime, humanize, money } from "@/lib/format";

import { CrudPage } from "./crud";

const active = (v: boolean | undefined) => <StatusBadge value={v === false ? "inactive" : "active"} />;
const published = (v: boolean | undefined) => (v ? <Badge variant="success">Published</Badge> : <Badge variant="secondary">Draft</Badge>);

export function ServiceAreasPage() {
  return (
    <CrudPage<Schemas["ServiceArea"]>
      resource="service-areas"
      title="Service areas"
      description="Where the platform takes work. Public pages are generated per category and area."
      noun="service area"
      columns={[
        { header: "Name", cell: (a) => a.name },
        { header: "Slug", cell: (a) => <code className="text-xs">{a.slug}</code> },
        { header: "County", cell: (a) => a.county ?? "—" },
        { header: "Radius", cell: (a) => (a.radius_km ? `${a.radius_km} km` : "—") },
        { header: "Status", cell: (a) => active(a.is_active) },
      ]}
      fields={[
        { name: "name", label: "Name", required: true },
        { name: "slug", label: "URL slug", required: true, placeholder: "kilimani" },
        { name: "county", label: "County" },
        { name: "centre_lat", label: "Centre latitude", type: "number" },
        { name: "centre_lng", label: "Centre longitude", type: "number" },
        { name: "radius_km", label: "Radius (km)", type: "number" },
        { name: "is_active", label: "Active", type: "checkbox" },
      ]}
    />
  );
}

function notice(minutes: number) {
  if (!minutes) return "None";
  if (minutes % 1440 === 0) return `${minutes / 1440} day${minutes === 1440 ? "" : "s"}`;
  if (minutes % 60 === 0) return `${minutes / 60} hour${minutes === 60 ? "" : "s"}`;
  return `${minutes} min`;
}

export function CategoriesPage() {
  return (
    <CrudPage<Schemas["Category"]>
      resource="categories"
      title="Categories"
      description="Service lines such as house help, cleaning or caregiving. The vetting tier decides how deep vetting goes."
      noun="category"
      columns={[
        { header: "Name", cell: (c) => c.name },
        { header: "Slug", cell: (c) => <code className="text-xs">{c.slug}</code> },
        { header: "Engagement", cell: (c) => humanize(c.engagement) },
        { header: "Vetting tier", cell: (c) => humanize(c.vetting_tier) },
        { header: "Client ID needed", cell: (c) => (c.requires_client_id_verification ? "Yes" : "No") },
        { header: "Visits start", cell: (c) => (c.opens_at && c.closes_at ? `${c.opens_at}–${c.closes_at}` : "Any time") },
        { header: "Notice", cell: (c) => notice(c.min_lead_minutes ?? 0) },
        { header: "Order", cell: (c) => c.sort_order ?? "—" },
        { header: "Status", cell: (c) => active(c.is_active) },
      ]}
      fields={[
        { name: "name", label: "Name", required: true },
        { name: "name_sw", label: "Name in Swahili" },
        { name: "slug", label: "URL slug", required: true },
        { name: "description", label: "Description", type: "textarea" },
        { name: "engagement", label: "Engagement", type: "select", options: EngagementType, required: true },
        { name: "vetting_tier", label: "Vetting tier", type: "select", options: VettingTier },
        { name: "requires_client_id_verification", label: "Client must verify their ID", type: "checkbox" },
        { name: "opens_at", label: "Earliest start time", type: "time", help: "Nairobi time. Leave both times empty to allow any time of day." },
        { name: "closes_at", label: "Latest start time", type: "time" },
        { name: "min_lead_minutes", label: "Notice needed (minutes)", type: "number", help: "How far ahead a visit must be booked, for example 120 for two hours." },
        { name: "sop_markdown", label: "Standard operating procedure (Markdown)", type: "textarea" },
        { name: "sort_order", label: "Sort order", type: "number" },
        { name: "is_active", label: "Active", type: "checkbox" },
      ]}
    />
  );
}

export function ServicesPage() {
  const categoryName = useCategoryName();
  const categories = useCategoryOptions();
  const { data: cats } = useCategories();
  const inactiveCats = new Set((cats ?? []).filter((c) => !c.is_active).map((c) => c.id));
  return (
    <CrudPage<Schemas["Service"]>
      resource="services"
      title="Services"
      description="What clients book within a category. Prices are set on the rate card."
      noun="service"
      columns={[
        { header: "Name", cell: (s) => s.name },
        { header: "Category", cell: (s) => categoryName(s.category_id) },
        { header: "Unit", cell: (s) => humanize(s.pricing_unit) },
        { header: "Duration", cell: (s) => (s.default_duration_minutes ? `${s.default_duration_minutes} min` : "—") },
        {
          header: "Current price",
          cell: (s) =>
            s.current_price ? money(s.current_price.client_price_minor) : <span className="text-muted-foreground text-xs">No all-area price</span>,
          className: "tabular-nums",
        },
        {
          header: "Status",
          cell: (s) =>
            s.is_active && inactiveCats.has(s.category_id) ? (
              <span className="text-xs text-amber-700 dark:text-amber-300">Category inactive: can't be booked</span>
            ) : (
              active(s.is_active)
            ),
        },
      ]}
      fields={[
        { name: "category_id", label: "Category", type: "select", options: categories, required: true },
        { name: "name", label: "Name", required: true },
        { name: "slug", label: "URL slug", required: true },
        { name: "description", label: "Description", type: "textarea" },
        { name: "scope_items", label: "What is included (one per line)", type: "lines" },
        { name: "pricing_unit", label: "Pricing unit", type: "select", options: PricingUnit, required: true },
        { name: "min_quantity", label: "Minimum quantity", type: "number" },
        { name: "max_quantity", label: "Maximum quantity", type: "number" },
        { name: "default_duration_minutes", label: "Default duration (minutes)", type: "number" },
        { name: "is_active", label: "Active", type: "checkbox" },
      ]}
    />
  );
}

export function SubscriptionPlansPage() {
  return (
    <CrudPage<Schemas["SubscriptionPlan"]>
      resource="subscription-plans"
      title="Subscription plans"
      description="Household plans paid by M-Pesa each period, with the benefits shown on the pricing page."
      noun="plan"
      columns={[
        { header: "Code", cell: (p) => <code className="text-xs">{p.code}</code> },
        { header: "Name", cell: (p) => p.name },
        { header: "Price", cell: (p) => money(p.price_minor), className: "tabular-nums" },
        { header: "Billing", cell: (p) => humanize(p.billing_period) },
        { header: "Status", cell: (p) => active(p.is_active) },
      ]}
      fields={[
        { name: "code", label: "Code", required: true },
        { name: "name", label: "Name", required: true },
        { name: "description", label: "Description", type: "textarea" },
        { name: "price_minor", label: "Price (KES)", type: "money", required: true },
        { name: "billing_period", label: "Billing period", type: "select", options: ["monthly", "quarterly", "annual"], required: true },
        { name: "benefits", label: "Benefits (JSON)", type: "json" },
        { name: "is_active", label: "Active", type: "checkbox" },
      ]}
    />
  );
}

export function TrainingCoursesPage() {
  const categoryName = useCategoryName();
  const categories = useCategoryOptions();
  return (
    <CrudPage<Schemas["TrainingCourse"]>
      resource="training-courses"
      title="Training courses"
      description="Induction courses gate activation. Retraining courses are used on the disciplinary ladder."
      noun="course"
      canDelete={false}
      columns={[
        { header: "Code", cell: (c) => <code className="text-xs">{c.code}</code> },
        { header: "Name", cell: (c) => c.name },
        { header: "Category", cell: (c) => (c.category_id ? categoryName(c.category_id) : "All") },
        { header: "Induction", cell: (c) => (c.is_induction ? "Yes" : "No") },
        { header: "Pass mark", cell: (c) => c.pass_mark ?? "—" },
        { header: "Status", cell: (c) => active(c.is_active) },
      ]}
      fields={[
        { name: "code", label: "Code", required: true },
        { name: "name", label: "Name", required: true },
        { name: "category_id", label: "Category", type: "select", options: categories },
        { name: "is_induction", label: "Induction course", type: "checkbox" },
        { name: "pass_mark", label: "Pass mark", type: "number" },
        { name: "is_active", label: "Active", type: "checkbox" },
      ]}
    />
  );
}

export function ContentPagesPage() {
  const categories = useCategoryOptions();
  const areas = useAreaOptions();
  const categoryName = useCategoryName();
  const areaName = useAreaName();
  return (
    <CrudPage<Schemas["ContentPage"]>
      resource="content-pages"
      title="Website pages"
      description="Pages served by the public site, including local service pages such as cleaners in Kilimani."
      noun="page"
      columns={[
        { header: "Title", cell: (p) => p.title },
        { header: "Slug", cell: (p) => <code className="text-xs">/{p.slug}</code> },
        { header: "Language", cell: (p) => p.language.toUpperCase() },
        { header: "Category", cell: (p) => (p.category_id ? categoryName(p.category_id) : "—") },
        { header: "Area", cell: (p) => (p.service_area_id ? areaName(p.service_area_id) : "—") },
        { header: "State", cell: (p) => published(p.is_published) },
        { header: "Updated", cell: (p) => dateTime(p.updated_at) },
      ]}
      fields={[
        { name: "title", label: "Title", required: true },
        { name: "slug", label: "URL slug", required: true },
        { name: "language", label: "Language", type: "select", options: Language },
        { name: "meta_description", label: "Search description", type: "textarea", help: "Shown by Google under the title. About 150 characters." },
        { name: "body_markdown", label: "Body (Markdown)", type: "textarea", required: true },
        { name: "category_id", label: "Category", type: "select", options: categories },
        { name: "service_area_id", label: "Service area", type: "select", options: areas },
        { name: "is_published", label: "Published", type: "checkbox" },
      ]}
    />
  );
}

export function FaqsPage() {
  const categories = useCategoryOptions();
  const categoryName = useCategoryName();
  return (
    <CrudPage<Schemas["Faq"]>
      resource="faqs"
      title="FAQs"
      noun="FAQ"
      columns={[
        { header: "Question", cell: (q) => <span className="line-clamp-2 max-w-md">{q.question}</span> },
        { header: "Category", cell: (q) => (q.category_id ? categoryName(q.category_id) : "General") },
        { header: "Language", cell: (q) => (q.language ?? "en").toUpperCase() },
        { header: "Order", cell: (q) => q.sort_order ?? "—" },
        { header: "State", cell: (q) => published(q.is_published) },
      ]}
      fields={[
        { name: "question", label: "Question", required: true },
        { name: "answer", label: "Answer", type: "textarea", required: true },
        { name: "category_id", label: "Category", type: "select", options: categories },
        { name: "language", label: "Language", type: "select", options: Language },
        { name: "sort_order", label: "Sort order", type: "number" },
        { name: "is_published", label: "Published", type: "checkbox" },
      ]}
    />
  );
}

export function NotificationTemplatesPage() {
  return (
    <CrudPage<Schemas["NotificationTemplate"]>
      resource="notification-templates"
      title="Message templates"
      description="Text for SMS, WhatsApp, email and push by platform event. Not used for sending yet: today's messages are written in the platform, so an empty list does not stop anything being sent."
      noun="template"
      columns={[
        { header: "Event", cell: (t) => <code className="text-xs">{t.event_key}</code> },
        { header: "Channel", cell: (t) => humanize(t.channel) },
        { header: "Language", cell: (t) => t.language.toUpperCase() },
        { header: "Body", cell: (t) => <span className="line-clamp-2 max-w-md text-xs">{t.body}</span> },
        { header: "Status", cell: (t) => active(t.is_active) },
      ]}
      fields={[
        { name: "event_key", label: "Event key", required: true, placeholder: "offer.sent" },
        { name: "channel", label: "Channel", type: "select", options: NotificationChannel, required: true },
        { name: "language", label: "Language", type: "select", options: Language, required: true },
        { name: "subject", label: "Subject (email and push)" },
        { name: "body", label: "Body", type: "textarea", required: true },
        { name: "is_active", label: "Active", type: "checkbox" },
      ]}
    />
  );
}

