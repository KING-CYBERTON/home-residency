import type { ReactNode } from "react";

import { AuditLogPage } from "@/pages/admin/audit";
import { ReportsPage } from "@/pages/admin/reports";
import { SettingsPage } from "@/pages/admin/settings";
import { StaffPage } from "@/pages/admin/staff";
import { ChecklistEditorPage } from "@/pages/catalogue/checklist-editor";
import { ChecklistsPage } from "@/pages/catalogue/checklists";
import {
  CategoriesPage,
  ContentPagesPage,
  FaqsPage,
  NotificationTemplatesPage,
  ServiceAreasPage,
  ServicesPage,
  SubscriptionPlansPage,
  TrainingCoursesPage,
} from "@/pages/catalogue/pages";
import { RateCardPage } from "@/pages/catalogue/rate-card";
import { ClientPage, ClientsPage } from "@/pages/clients/clients";
import { ComplaintsPage } from "@/pages/complaints/list";
import { ComplaintPage } from "@/pages/complaints/show";
import { SpotChecksPage } from "@/pages/complaints/spot-checks";
import { DashboardPage } from "@/pages/dashboard";
import { InvoicePage, InvoicesPage } from "@/pages/finance/invoices";
import {
  EarningsPage,
  PaymentsPage,
  RefundsPage,
} from "@/pages/finance/payments";
import { PayoutBatchPage, PayoutBatchesPage } from "@/pages/finance/payouts";
import { ReconciliationPage } from "@/pages/finance/reconciliation";
import { ProvidersPage } from "@/pages/providers/list";
import { ProviderPage } from "@/pages/providers/show";
import { TrainingsPage } from "@/pages/training/enrolments";
import { LeadPage, LeadsPage } from "@/pages/triage/leads";
import { ExceptionsPage, VariationsPage } from "@/pages/triage/lists";
import { RequestQueuePage } from "@/pages/triage/queue";
import { RequestPage } from "@/pages/triage/request";
import { ApplicationPage } from "@/pages/vetting/application";
import { ApplicationsPage } from "@/pages/vetting/applications";
import { AccessLogPage } from "@/pages/vetting/documents";
import { ExpiringDocumentsPage } from "@/pages/vetting/expiring";

/** Every console page, with the operation that guards it (see Guard in components/layout). */
export const routes: { path: string; op: string; element: ReactNode }[] = [
  { path: "/", op: "GET /console/dashboard", element: <DashboardPage /> },

  {
    path: "/requests",
    op: "GET /console/requests",
    element: <RequestQueuePage />,
  },
  {
    path: "/requests/:id",
    op: "GET /console/requests/{id}",
    element: <RequestPage />,
  },
  {
    path: "/variations",
    op: "GET /console/variations",
    element: <VariationsPage />,
  },
  {
    path: "/exceptions",
    op: "GET /console/exceptions",
    element: <ExceptionsPage />,
  },
  { path: "/leads", op: "GET /console/leads", element: <LeadsPage /> },
  { path: "/leads/:id", op: "GET /console/leads/{id}", element: <LeadPage /> },

  {
    path: "/applications",
    op: "GET /console/applications",
    element: <ApplicationsPage />,
  },
  {
    path: "/applications/:id",
    op: "GET /console/applications/{id}",
    element: <ApplicationPage />,
  },
  {
    path: "/expiring-documents",
    op: "GET /console/compliance/expiring-documents",
    element: <ExpiringDocumentsPage />,
  },
  {
    path: "/documents/:id/access-log",
    op: "GET /console/documents/{id}/access-log",
    element: <AccessLogPage />,
  },

  {
    path: "/providers",
    op: "GET /console/providers",
    element: <ProvidersPage />,
  },
  {
    path: "/providers/:id",
    op: "GET /console/providers/{id}",
    element: <ProviderPage />,
  },
  {
    path: "/trainings",
    op: "GET /console/trainings",
    element: <TrainingsPage />,
  },
  {
    path: "/training-courses",
    op: "GET /console/training-courses",
    element: <TrainingCoursesPage />,
  },

  { path: "/clients", op: "GET /console/clients", element: <ClientsPage /> },
  {
    path: "/clients/:id",
    op: "GET /console/clients/{id}",
    element: <ClientPage />,
  },

  {
    path: "/complaints",
    op: "GET /console/complaints",
    element: <ComplaintsPage />,
  },
  {
    path: "/complaints/:id",
    op: "GET /console/complaints/{id}",
    element: <ComplaintPage />,
  },
  {
    path: "/spot-checks",
    op: "GET /console/spot-checks",
    element: <SpotChecksPage />,
  },

  { path: "/invoices", op: "GET /console/invoices", element: <InvoicesPage /> },
  {
    path: "/invoices/:id",
    op: "GET /console/invoices/{id}",
    element: <InvoicePage />,
  },
  { path: "/payments", op: "GET /console/payments", element: <PaymentsPage /> },
  { path: "/refunds", op: "GET /console/refunds", element: <RefundsPage /> },
  { path: "/earnings", op: "GET /console/earnings", element: <EarningsPage /> },
  {
    path: "/payout-batches",
    op: "GET /console/payout-batches",
    element: <PayoutBatchesPage />,
  },
  {
    path: "/payout-batches/:id",
    op: "GET /console/payout-batches/{id}",
    element: <PayoutBatchPage />,
  },
  {
    path: "/reconciliation",
    op: "GET /console/reconciliation",
    element: <ReconciliationPage />,
  },

  {
    path: "/service-areas",
    op: "GET /console/service-areas",
    element: <ServiceAreasPage />,
  },
  {
    path: "/categories",
    op: "GET /console/categories",
    element: <CategoriesPage />,
  },
  { path: "/services", op: "GET /console/services", element: <ServicesPage /> },
  {
    path: "/rate-card",
    op: "GET /console/rate-card",
    element: <RateCardPage />,
  },
  {
    path: "/checklists",
    op: "GET /console/checklists",
    element: <ChecklistsPage />,
  },
  {
    path: "/checklists/new",
    op: "POST /console/checklists",
    element: <ChecklistEditorPage />,
  },
  {
    path: "/subscription-plans",
    op: "GET /console/subscription-plans",
    element: <SubscriptionPlansPage />,
  },
  {
    path: "/content-pages",
    op: "GET /console/content-pages",
    element: <ContentPagesPage />,
  },
  { path: "/faqs", op: "GET /console/faqs", element: <FaqsPage /> },
  {
    path: "/notification-templates",
    op: "GET /console/notification-templates",
    element: <NotificationTemplatesPage />,
  },

  {
    path: "/reports",
    op: "GET /console/reports/{report}",
    element: <ReportsPage />,
  },
  { path: "/staff", op: "GET /console/staff", element: <StaffPage /> },
  { path: "/settings", op: "GET /console/settings", element: <SettingsPage /> },
  {
    path: "/audit-log",
    op: "GET /console/audit-log",
    element: <AuditLogPage />,
  },
  {
    path: "*",
    op: "GET /console/dashboard",
    element: (
      <p className="text-muted-foreground py-20 text-center">Page not found.</p>
    ),
  },
];
