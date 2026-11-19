// Presentation only. The fixture API and bound target catalog enforce scope.
export const isolatedPrivate = import.meta.env.VITE_PRIVATE_LOOP === "true";
export const isolatedDemo = isolatedPrivate || import.meta.env.VITE_ISOLATED_DEMO === "true";
// Display context only. Per-monitor execution evidence always comes from the API.
export const privateBeta = !isolatedDemo && import.meta.env.VITE_PRIVATE_BETA === "true";
