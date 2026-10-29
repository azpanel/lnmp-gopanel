export type UserAgentBrand = {
  id: string;
  name: string;
  icon: string;
  monochrome?: boolean;
};

const brands = {
  chrome: { id: "chrome", name: "Chrome", icon: "/brands/chrome.svg" },
  edge: { id: "edge", name: "Microsoft Edge", icon: "/brands/edge.svg" },
  firefox: { id: "firefox", name: "Firefox", icon: "/brands/firefox.svg" },
  safari: { id: "safari", name: "Safari", icon: "/brands/safari.svg" },
  opera: { id: "opera", name: "Opera", icon: "/brands/opera.svg" },
  curl: {
    id: "curl",
    name: "curl",
    icon: "/brands/curl.svg",
    monochrome: true,
  },
  wget: {
    id: "wget",
    name: "GNU Wget",
    icon: "/brands/wget.svg",
    monochrome: true,
  },
  requests: {
    id: "requests",
    name: "Python requests",
    icon: "/brands/python3.svg",
  },
  go: { id: "go", name: "Go HTTP Client", icon: "/brands/go.svg" },
  okhttp: { id: "okhttp", name: "OkHttp", icon: "/brands/okhttp.png" },
  axios: {
    id: "axios",
    name: "axios",
    icon: "/brands/axios.svg",
    monochrome: true,
  },
} satisfies Record<string, UserAgentBrand>;

// More specific browsers precede the Chrome/Safari compatibility tokens.
// Require product/version tokens rather than names in arbitrary UA text.
const rules: ReadonlyArray<readonly [keyof typeof brands, ...RegExp[]]> = [
  ["curl", /^curl\/\d/i],
  ["wget", /^Wget\/\d/i],
  ["requests", /^python-requests\/\d/i],
  ["go", /^Go-http-client\/\d/i],
  ["okhttp", /^okhttp\/\d/i],
  ["axios", /^axios\/\d/i],
  ["edge", /(?:^|[\s;(])(?:Edg|EdgA|EdgiOS|Edge)\/\d/i],
  ["opera", /(?:^|[\s;(])(?:OPR|OPiOS|Opera(?: Mini| Mobi)?)\/\d/i],
  ["firefox", /(?:^|[\s;(])(?:Firefox|FxiOS)\/\d/i],
  ["chrome", /(?:^|[\s;(])(?:Chrome|CriOS)\/\d/i],
  ["safari", /(?:^|[\s;(])Version\/\d/i, /(?:^|[\s;(])Safari\/\d/i],
];

export function detectUserAgent(userAgent: string): UserAgentBrand | null {
  const value = userAgent.trim();
  for (const [id, ...patterns] of rules) {
    if (patterns.every((pattern) => pattern.test(value))) return brands[id];
  }
  return null;
}
