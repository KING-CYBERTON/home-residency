import {
  AlertTriangle,
  BadgeCheck,
  BarChart3,
  BookOpen,
  ClipboardCheck,
  ClipboardList,
  Coins,
  FileClock,
  FileText,
  GraduationCap,
  HandCoins,
  HelpCircle,
  Inbox,
  LayoutDashboard,
  ListChecks,
  type LucideIcon,
  Mail,
  MapPin,
  MessageSquareWarning,
  Receipt,
  RotateCcw,
  ScrollText,
  Settings,
  Tags,
  Undo2,
  UserCog,
  Users,
  Wallet,
  Wrench,
  Scale,
  Eye,
  Banknote,
  Layers,
  KeyRound,
  ShieldCheck,
} from "lucide-react";

export type NavItem = {
  /** Refine resource name; matches the /console/{name} collection where there is one. */
  name: string;
  label: string;
  route: string;
  /** The operation behind the page; the item is shown only to roles that may call it. */
  op: string;
  icon: LucideIcon;
  group: string;
};

export const nav: NavItem[] = [
  { name: "dashboard", label: "Dashboard", route: "/", op: "GET /console/dashboard", icon: LayoutDashboard, group: "" },

  { name: "requests", label: "Request queue", route: "/requests", op: "GET /console/requests", icon: Inbox, group: "Triage" },
  { name: "variations", label: "Variations", route: "/variations", op: "GET /console/variations", icon: Layers, group: "Triage" },
  { name: "exceptions", label: "Exception log", route: "/exceptions", op: "GET /console/exceptions", icon: AlertTriangle, group: "Triage" },
  { name: "leads", label: "Leads", route: "/leads", op: "GET /console/leads", icon: Mail, group: "Triage" },

  { name: "applications", label: "Applications", route: "/applications", op: "GET /console/applications", icon: ClipboardCheck, group: "Vetting" },
  { name: "expiring-documents", label: "Expiring documents", route: "/expiring-documents", op: "GET /console/compliance/expiring-documents", icon: FileClock, group: "Vetting" },

  { name: "providers", label: "Providers", route: "/providers", op: "GET /console/providers", icon: BadgeCheck, group: "Providers" },
  { name: "trainings", label: "Training", route: "/trainings", op: "GET /console/trainings", icon: GraduationCap, group: "Providers" },
  { name: "training-courses", label: "Courses", route: "/training-courses", op: "GET /console/training-courses", icon: BookOpen, group: "Providers" },

  { name: "clients", label: "Clients", route: "/clients", op: "GET /console/clients", icon: Users, group: "Clients" },

  { name: "complaints", label: "Complaints", route: "/complaints", op: "GET /console/complaints", icon: MessageSquareWarning, group: "Quality" },
  { name: "spot-checks", label: "Spot checks", route: "/spot-checks", op: "GET /console/spot-checks", icon: Eye, group: "Quality" },

  { name: "invoices", label: "Invoices", route: "/invoices", op: "GET /console/invoices", icon: Receipt, group: "Finance" },
  { name: "payments", label: "Payments", route: "/payments", op: "GET /console/payments", icon: Wallet, group: "Finance" },
  { name: "refunds", label: "Refunds", route: "/refunds", op: "GET /console/refunds", icon: Undo2, group: "Finance" },
  { name: "earnings", label: "Earnings", route: "/earnings", op: "GET /console/earnings", icon: HandCoins, group: "Finance" },
  { name: "payout-batches", label: "Payouts", route: "/payout-batches", op: "GET /console/payout-batches", icon: Banknote, group: "Finance" },
  { name: "reconciliation", label: "Ledger", route: "/reconciliation", op: "GET /console/reconciliation", icon: Scale, group: "Finance" },

  { name: "service-areas", label: "Service areas", route: "/service-areas", op: "GET /console/service-areas", icon: MapPin, group: "Catalogue" },
  { name: "categories", label: "Categories", route: "/categories", op: "GET /console/categories", icon: Tags, group: "Catalogue" },
  { name: "services", label: "Services", route: "/services", op: "GET /console/services", icon: Wrench, group: "Catalogue" },
  { name: "rate-card", label: "Rate card", route: "/rate-card", op: "GET /console/rate-card", icon: Coins, group: "Catalogue" },
  { name: "checklists", label: "Vetting checklists", route: "/checklists", op: "GET /console/checklists", icon: ListChecks, group: "Catalogue" },
  { name: "subscription-plans", label: "Subscription plans", route: "/subscription-plans", op: "GET /console/subscription-plans", icon: RotateCcw, group: "Catalogue" },

  { name: "content-pages", label: "Pages", route: "/content-pages", op: "GET /console/content-pages", icon: FileText, group: "Content" },
  { name: "faqs", label: "FAQs", route: "/faqs", op: "GET /console/faqs", icon: HelpCircle, group: "Content" },
  { name: "notification-templates", label: "Message templates", route: "/notification-templates", op: "GET /console/notification-templates", icon: ClipboardList, group: "Content" },

  { name: "reports", label: "Reports", route: "/reports", op: "GET /console/reports/{report}", icon: BarChart3, group: "Admin" },
  { name: "staff", label: "Staff", route: "/staff", op: "GET /console/staff", icon: UserCog, group: "Admin" },
  { name: "settings", label: "Settings", route: "/settings", op: "GET /console/settings", icon: Settings, group: "Admin" },
  { name: "audit-log", label: "Audit log", route: "/audit-log", op: "GET /console/audit-log", icon: ScrollText, group: "Admin" },

  // Every staff user can manage their own sign-in and read what each role may do.
  { name: "account", label: "My account", route: "/account", op: "GET /console/dashboard", icon: KeyRound, group: "Account" },
  { name: "roles", label: "Roles", route: "/roles", op: "GET /console/dashboard", icon: ShieldCheck, group: "Account" },
];
