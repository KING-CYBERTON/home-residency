import { SITE } from "./env";
import { one } from "./db";

// The API runs with the in-memory M-Pesa fake (no MPESA_* settings), so the tests play Safaricom:
// they post the callbacks Daraja would send, in Daraja's own format.

async function post(path: string, body: unknown) {
  const res = await fetch(`${SITE}/api/v1/webhooks/mpesa/${path}`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  if (!res.ok) throw new Error(`mpesa ${path}: ${res.status} ${await res.text()}`);
}

/** The checkout id of the newest STK push for an invoice. The payment row is written before the push
 *  goes out and the id is filled in after it returns, so this waits for the id. */
export async function latestCheckout(invoiceId: string): Promise<string> {
  for (let i = 0; i < 50; i++) {
    const row = await one<{ c: string | null }>(`SELECT mpesa_checkout_request_id AS c FROM payments WHERE invoice_id = $1 ORDER BY initiated_at DESC LIMIT 1`, invoiceId);
    if (row.c) return row.c;
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(`no STK checkout id for invoice ${invoiceId}`);
}

export async function stkCallback(checkout: string, amountKes: number, receipt: string, resultCode = 0) {
  await post("stk", {
    Body: { stkCallback: {
      MerchantRequestID: "e2e", CheckoutRequestID: checkout, ResultCode: resultCode,
      ResultDesc: resultCode === 0 ? "The service request is processed successfully." : "Request cancelled by user",
      ...(resultCode === 0 ? { CallbackMetadata: { Item: [
        { Name: "Amount", Value: amountKes }, { Name: "MpesaReceiptNumber", Value: receipt },
        { Name: "TransactionDate", Value: 20261015120000 }, { Name: "PhoneNumber", Value: 254711000000 },
      ] } } : {}),
    } },
  });
}

/** Result of a B2C payout or refund, matched by the conversation id the fake handed out. */
export async function b2cResult(conversationId: string, amountKes: number, ok = true) {
  await post("b2c/result", {
    Result: {
      ResultType: 0, ResultCode: ok ? 0 : 2001, ResultDesc: ok ? "The service request is processed successfully." : "The initiator information is invalid.",
      OriginatorConversationID: "e2e", ConversationID: conversationId, TransactionID: `E2E${conversationId.slice(-6)}`,
      ResultParameters: { ResultParameter: [{ Key: "TransactionReceipt", Value: `R${conversationId.slice(-8)}` }, { Key: "TransactionAmount", Value: amountKes }] },
    },
  });
}
