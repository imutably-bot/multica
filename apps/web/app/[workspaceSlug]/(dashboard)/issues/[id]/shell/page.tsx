"use client";

import "@xterm/xterm/css/xterm.css";

import Link from "next/link";
import { use, useEffect, useRef, useState, useTransition } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "next/navigation";
import { ArrowLeft, Loader2, RefreshCcw, Terminal } from "lucide-react";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal as XTerm } from "@xterm/xterm";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import { buttonVariants } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

type ShellSession = {
  session_id: string;
  state: string;
  work_dir?: string;
  error?: string;
};

type ShellFrame =
  | {
      type: "snapshot";
      snapshot: ShellSession;
      data?: string;
    }
  | {
      type: "output";
      data: string;
    }
  | {
      type: "error";
      error: string;
    };

export default function IssueShellPage({
  params,
}: {
  params: Promise<{ workspaceSlug: string; id: string }>;
}) {
  const { workspaceSlug, id } = use(params);
  return (
    <ErrorBoundary resetKeys={[id]}>
      <IssueShellPageInner issueId={id} workspaceSlug={workspaceSlug} />
    </ErrorBoundary>
  );
}

function IssueShellPageInner({
  issueId,
  workspaceSlug,
}: {
  issueId: string;
  workspaceSlug: string;
}) {
  const wsId = useWorkspaceId();
  const searchParams = useSearchParams();
  const agentId = searchParams.get("agentId") ?? undefined;

  const [session, setSession] = useState<ShellSession | null>(null);
  const [transportError, setTransportError] = useState<string | null>(null);
  const [isPending, startTransition] = useTransition();
  const terminalHostRef = useRef<HTMLDivElement | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const terminalRef = useRef<XTerm | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const sessionRef = useRef<ShellSession | null>(null);

  const { data: issue, isLoading: issueLoading } = useQuery(issueDetailOptions(wsId, issueId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const activeAgent = agents.find((a) => a.id === (agentId ?? issue?.assignee_id ?? "")) ?? null;

  useEffect(() => {
    sessionRef.current = session;
  }, [session]);

  function connectSocket(sessionId: string) {
    socketRef.current?.close();
    const protocol = window.location.protocol === "https:" ? "wss" : "ws";
    const agentParam = agentId ? `&agent_id=${encodeURIComponent(agentId)}` : "";
    const socket = new WebSocket(
      `${protocol}://${window.location.host}/api/issues/${issueId}/shell/ws?workspace_slug=${encodeURIComponent(workspaceSlug)}${agentParam}`,
    );
    socketRef.current = socket;

    socket.onopen = () => {
      setTransportError(null);
      if (terminalRef.current) {
        fitRef.current?.fit();
        socket.send(
          JSON.stringify({
            type: "resize",
            cols: terminalRef.current.cols,
            rows: terminalRef.current.rows,
          }),
        );
      }
    };

    socket.onmessage = (event) => {
      const frame = JSON.parse(event.data) as ShellFrame;
      switch (frame.type) {
        case "snapshot":
          if (frame.snapshot.session_id !== sessionId) return;
          setSession(frame.snapshot);
          setTransportError(frame.snapshot.error ?? null);
          if (frame.data) {
            terminalRef.current?.write(frame.data);
          }
          break;
        case "output":
          terminalRef.current?.write(frame.data);
          break;
        case "error":
          setTransportError(frame.error);
          break;
      }
    };

    socket.onclose = () => {
      if (sessionRef.current?.state === "running") {
        setTransportError("Shell connection closed");
      }
    };
  }

  useEffect(() => {
    if (!terminalHostRef.current || terminalRef.current) {
      return;
    }

    const term = new XTerm({
      convertEol: true,
      cursorBlink: true,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
      fontSize: 13,
      theme: {
        background: "#09090b",
        foreground: "#f4f4f5",
      },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(terminalHostRef.current);
    fit.fit();

    terminalRef.current = term;
    fitRef.current = fit;

    const resizeObserver = new ResizeObserver(() => {
      fit.fit();
      if (socketRef.current?.readyState === WebSocket.OPEN) {
        socketRef.current.send(
          JSON.stringify({
            type: "resize",
            cols: term.cols,
            rows: term.rows,
          }),
        );
      }
    });
    resizeObserver.observe(terminalHostRef.current);

    const disposeData = term.onData((data) => {
      if (socketRef.current?.readyState === WebSocket.OPEN) {
        socketRef.current.send(JSON.stringify({ type: "input", data }));
      }
    });

    return () => {
      disposeData.dispose();
      resizeObserver.disconnect();
      socketRef.current?.close();
      socketRef.current = null;
      term.dispose();
      terminalRef.current = null;
      fitRef.current = null;
    };
  }, []);

  useEffect(() => {
    if (!issueId || !terminalRef.current) {
      return;
    }

    let cancelled = false;
    startTransition(() => {
      void api
        .createIssueShellSession(issueId, agentId)
        .then((created) => {
          if (cancelled) return;
          setSession(created);
          setTransportError(created.error ?? null);
          terminalRef.current?.clear();
          connectSocket(created.session_id);
        })
        .catch((error: Error) => {
          if (cancelled) return;
          setTransportError(error.message || "Failed to open issue shell");
        });
    });

    return () => {
      cancelled = true;
    };
  }, [issueId, agentId]);

  const reconnectShell = () => {
    setTransportError(null);
    terminalRef.current?.clear();
    startTransition(() => {
      void api
        .createIssueShellSession(issueId, agentId)
        .then((created) => {
          setSession(created);
          connectSocket(created.session_id);
        })
        .catch((error: Error) => {
          setTransportError(error.message || "Failed to reopen issue shell");
        });
    });
  };

  if (issueLoading || !issue) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="mx-auto flex h-[calc(100vh-7rem)] w-full max-w-6xl flex-col gap-4 px-4 py-4 md:px-6">
      <div className="flex items-center justify-between gap-3 rounded-2xl border bg-card px-4 py-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Terminal className="h-4 w-4" />
            <span>Issue shell</span>
          </div>
          <div className="truncate text-lg font-semibold">
            {issue.identifier}: {issue.title}
          </div>
          <div className="text-sm text-muted-foreground">
            {activeAgent ? `Runtime shell for ${activeAgent.name}` : "Waiting for assigned agent"}
          </div>
          {session?.work_dir && (
            <div className="truncate text-xs text-muted-foreground">{session.work_dir}</div>
          )}
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={reconnectShell}
            className={cn(buttonVariants({ variant: "outline", size: "sm" }))}
            disabled={isPending}
          >
            <RefreshCcw className="h-4 w-4" />
            Reconnect
          </button>
          <Link
            href={`/${workspaceSlug}/issues/${issue.id}`}
            className={cn(buttonVariants({ variant: "outline", size: "sm" }))}
          >
            <ArrowLeft className="h-4 w-4" />
            Back to issue
          </Link>
        </div>
      </div>

      <div className="grid min-h-0 flex-1 gap-3 lg:grid-cols-[1fr_260px]">
        <div className="relative min-h-0 overflow-hidden rounded-3xl border bg-black">
          <div ref={terminalHostRef} className="h-full w-full px-3 py-3" />
          {(isPending || session?.state === "starting") && (
            <div className="pointer-events-none absolute inset-x-0 top-0 flex items-center gap-2 border-b border-white/10 bg-black/80 px-4 py-2 text-xs text-white/80">
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
              Starting shell on the assigned runtime
            </div>
          )}
        </div>

        <div className="rounded-3xl border bg-card p-4">
          <div className="text-sm font-medium">Session</div>
          <div className="mt-3 space-y-2 text-sm text-muted-foreground">
            <div>
              State: <span className="text-foreground">{session?.state ?? "starting"}</span>
            </div>
            <div>
              Agent: <span className="text-foreground">{activeAgent?.name ?? "Unassigned"}</span>
            </div>
            <div>
              Runtime: <span className="text-foreground">{activeAgent ? "connected" : "missing"}</span>
            </div>
            {transportError && (
              <div className="rounded-2xl border border-destructive/30 bg-destructive/5 px-3 py-2 text-destructive">
                {transportError}
              </div>
            )}
            <div className="rounded-2xl border bg-muted/40 px-3 py-2 text-xs leading-5">
              This opens the assigned runtime locally and keeps the frontend as a thin terminal view.
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
