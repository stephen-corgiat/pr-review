import React from "react";

// A button.
export function Button({ label }: { label: string }) {
  return <button>{label.toUpperCase()}</button>;
}

export const Panel = () => {
  const onClick = () => track("tap");
  return (
    <div onClick={onClick}>
      <Button label="a" />
    </div>
  );
};

export const Memo = React.memo(() => <span>memo!</span>);

function track(name: string) {
  console.info(name);
}
