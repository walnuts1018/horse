import { createFileRoute } from "@tanstack/react-router";
import { App } from "../horse-app";

export const Route = createFileRoute("/")({ component: App });
