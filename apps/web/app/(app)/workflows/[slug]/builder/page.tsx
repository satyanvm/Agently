"use client";

import * as React from "react";
import { useParams } from "next/navigation";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer } from "@/components/shell/page";
import { WorkflowBuilder } from "@/components/builder/workflow-builder";

export default function BuilderPage() {
  const { slug } = useParams<{ slug: string }>();

  return (
    <>
      <TopBar crumbs={[
        { label: "Workflows", href: "/workflows" },
        { label: slug, href: `/workflows/${slug}` },
        { label: "Builder" },
      ]} />
      <PageContainer className="!p-0 h-[calc(100vh-57px)]">
        <WorkflowBuilder workflowSlug={slug} />
      </PageContainer>
    </>
  );
}
