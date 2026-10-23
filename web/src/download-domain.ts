import { useQuery } from '@tanstack/react-query';
import { useGetIdentity } from 'react-admin';
import { api } from './api';

export type AssignedDownloadDomain = { mode: 'unified' | 'separate'; plan_id: 'free' | 'pro'; download_base_url: string };

export function useDownloadDomain() {
  const { data: identity } = useGetIdentity();
  return useQuery({
    queryKey: ['download-domain', identity?.id],
    queryFn: () => api<AssignedDownloadDomain>('/download-domain'),
    enabled: !!identity?.id,
    staleTime: 30000,
  });
}
