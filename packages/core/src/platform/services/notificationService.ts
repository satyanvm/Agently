import type {
  Notification,
  NotificationType,
  Severity,
  NotificationId,
  RunId,
  Page,
  ListNotificationsQuery,
} from "@agently/contracts";
import { DomainError, ids } from "@agently/contracts";
import type { PlatformDeps, Emitter } from "./context.js";
import { paginate, byNewest } from "../util.js";

export interface CreateNotificationInput {
  type: NotificationType;
  severity: Severity;
  title: string;
  body: string;
  workflowSlug?: string | null;
  runId?: RunId | null;
  runNumber?: number | null;
}

export interface NotificationService {
  list(query: ListNotificationsQuery): Page<Notification>;
  markRead(id: NotificationId): Notification;
  markAllRead(): { updated: number };
  /** Durable write path: raise a notification + emit an event. */
  create(input: CreateNotificationInput): Notification;
}

export function createNotificationService(deps: PlatformDeps, emit: Emitter): NotificationService {
  const { repos, clock } = deps;

  return {
    list(query) {
      let items = repos.notifications.all().sort(byNewest((n) => n.createdAt));
      if (query.unread) items = items.filter((n) => n.readAt === null);
      if (query.type) items = items.filter((n) => n.type === query.type);
      return paginate(items, query);
    },
    markRead(id) {
      const existing = repos.notifications.getById(id);
      if (!existing) throw DomainError.notFound("notification");
      if (existing.readAt) return existing;
      return repos.notifications.update(id, { readAt: clock.iso() });
    },
    markAllRead() {
      const updated = repos.notifications.markAllRead(repos.workspace.get().id, clock.iso());
      return { updated };
    },
    create(input) {
      const notification: Notification = {
        id: ids.notification(),
        workspaceId: repos.workspace.get().id,
        recipientId: repos.members.all()[0]?.id ?? null,
        type: input.type,
        severity: input.severity,
        title: input.title,
        body: input.body,
        workflowSlug: input.workflowSlug ?? null,
        runId: input.runId ?? null,
        runNumber: input.runNumber ?? null,
        readAt: null,
        createdAt: clock.iso(),
      };
      repos.notifications.insert(notification);
      emit({ type: "notification.created", notificationId: notification.id });
      return notification;
    },
  };
}
