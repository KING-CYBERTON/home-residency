import { $api } from "@/api/client";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { useCan } from "@/lib/access";

export function VariationDecisionButtons({ id }: { id: string }) {
  const decide = $api.useMutation("post", "/console/variations/{id}/decision");
  const can = useCan("POST /console/variations/{id}/decision");
  const call = (decision: "approve" | "reject") => (body: { reason?: string }) =>
    decide.mutateAsync({ params: { path: { id } }, body: { decision, ...body } });
  return (
    <Allowed when={can}>
      <FormDialog
        trigger="Approve"
        triggerVariant="default"
        title="Approve variation"
        description="The price change is added to the client's invoice and the provider's earning."
        fields={[{ name: "reason", label: "Note", type: "textarea" }]}
        onSubmit={call("approve")}
      />
      <FormDialog
        trigger="Reject"
        title="Reject variation"
        fields={[{ name: "reason", label: "Reason", type: "textarea", required: true }]}
        destructive
        onSubmit={call("reject")}
      />
    </Allowed>
  );
}

export function ExceptionResolveButton({ id }: { id: string }) {
  const resolve = $api.useMutation("post", "/console/exceptions/{id}/resolve");
  const can = useCan("POST /console/exceptions/{id}/resolve");
  return (
    <Allowed when={can}>
      <FormDialog
        trigger="Resolve"
        title="Resolve exception"
        fields={[
          { name: "resolution", label: "What was done", type: "textarea", required: true },
          { name: "open_complaint", label: "Also open a complaint", type: "checkbox" },
        ]}
        onSubmit={(body) => resolve.mutateAsync({ params: { path: { id } }, body })}
      />
    </Allowed>
  );
}
