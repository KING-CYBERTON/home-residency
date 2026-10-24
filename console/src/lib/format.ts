const kes = new Intl.NumberFormat("en-KE", { style: "currency", currency: "KES", minimumFractionDigits: 2 });

/** Money is carried as KES minor units (cents) everywhere in the API. */
export function money(minor: number | null | undefined): string {
  if (minor === null || minor === undefined) return "—";
  return kes.format(minor / 100);
}

const dateFmt = new Intl.DateTimeFormat("en-KE", { day: "numeric", month: "short", year: "numeric" });
const dateTimeFmt = new Intl.DateTimeFormat("en-KE", {
  day: "numeric",
  month: "short",
  hour: "2-digit",
  minute: "2-digit",
});

export function date(value: string | null | undefined): string {
  if (!value) return "—";
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? value : dateFmt.format(d);
}

export function dateTime(value: string | null | undefined): string {
  if (!value) return "—";
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? value : dateTimeFmt.format(d);
}

const rtf = new Intl.RelativeTimeFormat("en", { numeric: "auto" });

export function ago(value: string | null | undefined): string {
  if (!value) return "—";
  const diff = (new Date(value).getTime() - Date.now()) / 1000;
  const abs = Math.abs(diff);
  if (abs < 60) return rtf.format(Math.round(diff), "second");
  if (abs < 3600) return rtf.format(Math.round(diff / 60), "minute");
  if (abs < 86400) return rtf.format(Math.round(diff / 3600), "hour");
  return rtf.format(Math.round(diff / 86400), "day");
}

/** Words that keep their own spelling when an enum value is shown to people. */
const words: Record<string, string> = {
  id: "ID",
  kra: "KRA",
  pin: "PIN",
  nssf: "NSSF",
  shif: "SHIF",
  sha: "SHA",
  nhif: "NHIF",
  sms: "SMS",
  ussd: "USSD",
  otp: "OTP",
  vat: "VAT",
  mfa: "MFA",
  stk: "STK",
  mpesa: "M-Pesa",
  url: "URL",
};

/** "awaiting_client_choice" -> "Awaiting client choice", "kra_pin" -> "KRA PIN" */
export function humanize(value: string | null | undefined): string {
  if (!value) return "—";
  const s = value
    .split("_")
    .map((w) => words[w.toLowerCase()] ?? w)
    .join(" ");
  return s.charAt(0).toUpperCase() + s.slice(1);
}

export function shortId(id: string | null | undefined): string {
  return id ? id.slice(0, 8) : "—";
}

export function score(value: number | null | undefined, digits = 1): string {
  return value === null || value === undefined ? "—" : value.toFixed(digits);
}

/** yyyy-mm-dd for <input type="date">, in local time. */
export function isoDate(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function daysAgo(n: number): string {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return isoDate(d);
}
