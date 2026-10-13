import { Outlet, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/households/$householdId")({ component: Outlet });
