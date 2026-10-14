import type { NotificationProvider } from "@refinedev/core";
import { toast } from "sonner";

export const notificationProvider: NotificationProvider = {
  open({ key, message, description, type }) {
    const opts = { id: key, description };
    if (type === "success") toast.success(message, opts);
    else if (type === "error") toast.error(message, opts);
    else toast.loading(message, opts);
  },
  close(key) {
    toast.dismiss(key);
  },
};
