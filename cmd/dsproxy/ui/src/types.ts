export interface PolicyData {
  policies: string[][];
  groupingPolicies: string[][];
}

export interface PolicyRule {
  subject: string;
  domain: string;
  object: string;
  action: string;
}

export interface GroupingPolicyRule {
  user: string;
  role: string;
}
