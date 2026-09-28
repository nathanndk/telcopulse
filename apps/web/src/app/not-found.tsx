import Link from "next/link";
import { EmptyState } from "@/components/common";
import { Button } from "@/components/ui/button";
export default function NotFound() {
  return (
    <EmptyState
      title="Page not found"
      description="This workspace page does not exist."
    >
      <Button asChild>
        <Link href="/">Return to overview</Link>
      </Button>
    </EmptyState>
  );
}
