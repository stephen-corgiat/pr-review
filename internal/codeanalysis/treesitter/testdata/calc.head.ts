import { log } from "./log";

// Adds two numbers.
// Now logs too.
export function add(a: number, b: number): number {
  log("add");
  return helper(a) + b;
}

function helper(a: number): number {
  return a;
}

/** Doubles n. */
export const double = (n: number) => add(n, n);

export class Calc {
  constructor(private base: number) {
    this.reset();
  }

  /** Steps by n. */
  step(n: number): number {
    return add(this.base, double(n));
  }

  @logged()
  reset(): void {
    this.base = 1;
  }

  onClick = (n: number) => this.step(n);
}

function over(a: string): void;
function over(a: number): void;
function over(a: any): void {
  log(a);
}

export default function () {
  return 2;
}

function logged() {
  return (t: any) => t ?? null;
}
