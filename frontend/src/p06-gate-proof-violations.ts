// p06-gate-proof-violations.ts — EC-13 fixture file.
// Intentional violations to demonstrate P-06 CI gates.
// This entire file is DELETED in the follow-up clean commit.

// Gate #5 — tsc: type error — assigning string to number
const x: number = "this is not a number";

// Gate #4 — eslint: unused variable (no-unused-vars rule)
const unusedVar = "I am never used";

export {};
