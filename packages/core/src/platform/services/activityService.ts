import type { ActivityEvent, Page, PageQuery } from "@agently/contracts";
import type { PlatformDeps } from "./context.js";
import { paginate, byNewest } from "../util.js";

export interface ActivityService {
  list(query: PageQuery): Page<ActivityEvent>;
}

export function createActivityService(deps: PlatformDeps): ActivityService {
  const { repos } = deps;
  return {
    list(query) {
      const items = repos.activity.all().sort(byNewest((a) => a.at));
      return paginate(items, query);
    },
  };
}
