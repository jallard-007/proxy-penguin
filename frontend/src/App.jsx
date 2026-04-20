import { useTheme } from "./theme/ThemeContext";
import ThemeSync from "./theme/ThemeSync";
import { MyGrid } from "./Grid"
import { useEffect } from "react";

export default function App() {
    const { theme } = useTheme();

    return (
        <>
            <ThemeSync />
            <div className="h-screen flex flex-col bg-white text-black dark:bg-gray-900 dark:text-white">
                <div className="flex-1 min-h-0 p-4">
                    <div className="h-full w-full">
                        <MyGrid />
                    </div>
                </div>
            </div>
        </>
    );
}
