import React from "react";

// A button.
export function Button({ label }: { label: string }) {
  return <button>{label}</button>;
}

export const Panel = () => {
  const onClick = () => track("click");
  return (
    <div onClick={onClick}>
      <Button label="a" />
    </div>
  );
};

export const Memo = React.memo(() => <span>memo</span>);

function track(name: string) {
  console.log(name);
}
