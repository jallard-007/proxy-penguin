interface StatusBadgeProps {
  status: number;
}

export default function StatusBadge({ status }: StatusBadgeProps) {
  let colorClasses: string;
  if (status < 300) {
    colorClasses = 'bg-green-500/15 text-green-400';
  } else if (status < 400) {
    colorClasses = 'bg-blue-400/15 text-blue-400';
  } else if (status < 500) {
    colorClasses = 'bg-amber-500/15 text-amber-400';
  } else {
    colorClasses = 'bg-red-500/15 text-red-400';
  }

  return (
    <span className={`inline-block px-2 py-0.5 rounded-full text-xs font-semibold ${colorClasses}`}>
      {status}
    </span>
  );
}
