export type ClusterMember = {
  name: string;
  status: "ok" | "degraded" | "down";
};

export type ClusterStatus = {
  cluster?: {
    members?: ClusterMember[];
    leader?: string;
  };
  plugins?: Array<{ name: string; loaded: boolean }>;
};
