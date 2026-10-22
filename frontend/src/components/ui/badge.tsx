import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2",
  {
    variants: {
      variant: {
        default:
          "border-transparent bg-slate-900 text-slate-50 dark:bg-slate-100 dark:text-slate-900 hover:bg-slate-800",
        secondary:
          "border-transparent bg-slate-100 dark:bg-slate-800 text-slate-900 dark:text-slate-100 hover:bg-slate-200 dark:hover:bg-slate-700",
        destructive:
          "border-red-200 dark:border-red-900/60 bg-red-100 dark:bg-red-950/70 text-red-700 dark:text-red-300",
        outline: "text-slate-900 dark:text-slate-100 border-slate-200 dark:border-slate-800",
        success: "border-emerald-200 dark:border-emerald-900/60 bg-emerald-50 dark:bg-emerald-950/70 text-emerald-700 dark:text-emerald-300",
        warning: "border-amber-200 dark:border-amber-900/60 bg-amber-50 dark:bg-amber-950/70 text-amber-700 dark:text-amber-300",
        info: "border-blue-200 dark:border-blue-900/60 bg-blue-50 dark:bg-blue-950/70 text-blue-700 dark:text-blue-300",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
  return (
    <div className={cn(badgeVariants({ variant }), className)} {...props} />
  );
}

export { Badge, badgeVariants };
