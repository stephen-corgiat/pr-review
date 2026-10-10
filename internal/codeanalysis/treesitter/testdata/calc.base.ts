import { log } from "./log";

// Adds two numbers.
export function add(a: number, b: number): number {
  return a + b;
}

function helper(a: number): number {
  return a;
}

export class Calc {
  constructor(private base: number) {}

  /** Steps by n. */
  step(n: number): number {
    return add(this.base, n);
  }

  @logged()
  reset(): void {
    this.base = 0;
  }
}

function over(a: string): void;
function over(a: any): void {
  log(a);
}

export default function () {
  return 1;
}

function logged() {
  return (t: any) => t;
}
