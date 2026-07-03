export type GroupMember = {
  username: string;
  method: string;
};

export type Group = {
  uid?: string;
  name: string;
  description?: string;
  members?: GroupMember[];
};
