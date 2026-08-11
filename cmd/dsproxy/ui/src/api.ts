import { PolicyData } from './types';

export const fetchPolicy = async (): Promise<PolicyData> => {
  const response = await fetch('/api/policy');
  if (!response.ok) {
    throw new Error('Failed to fetch policy');
  }
  return response.json();
};

export const savePolicy = async (data: PolicyData): Promise<void> => {
  const response = await fetch('/api/policy', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(data),
  });
  if (!response.ok) {
    throw new Error('Failed to save policy');
  }
};
