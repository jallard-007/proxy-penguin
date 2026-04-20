import { useState, useEffect, useMemo, useRef } from "react";
import { themeQuartz, iconSetMaterial } from 'ag-grid-community';
import { AgGridReact } from 'ag-grid-react';

// Cell class rules for status column
const statusCellClassRules = {
    'status-info': (params) => params.value >= 100 && params.value < 199,
    'status-success': (params) => params.value >= 200 && params.value < 299,
    'status-redirect': (params) => params.value >= 300 && params.value < 399,
    'status-client': (params) => params.value >= 400 && params.value < 499,
    'status-server': (params) => params.value >= 500 && params.value < 599,
};

const myTheme = themeQuartz
    .withPart(iconSetMaterial)
    .withParams(
        {
            // Existing theme params...
        },
        'light' // Light scheme name
    )
    .withParams(
        {
            backgroundColor: '#1e1e2f',
            foregroundColor: '#e2e8f0',
            headerBackgroundColor: '#2d2d44',
            selectedRowBackgroundColor: 'rgba(110, 168, 254, 0.2)',
        },
        'dark' // Dark scheme name
    );

export const MyGrid = () => {
    const gridRef = useRef(null);
    const [gridReady, setGridReady] = useState(false);

    // const containerStyle = useMemo(() => ({ width: "100%", height: "100%" }), []);
    // const gridStyle = useMemo(() => ({ height: "100%", width: "100%" }), []);

    const [columnDefs, setColumnDefs] = useState([
        { headerName: "Timestamp", field: "t" },
        { headerName: "Hostname", field: "h" },
        { headerName: "Path", field: "p" },
        { headerName: "Query Path", field: "qp" },
        { headerName: "Client IP", field: "cip" },
        { headerName: "Status", field: "s", cellClassRules: statusCellClassRules },
        { headerName: "Duration", field: "d" },
        { headerName: "User Agent", field: "ua" },
    ]);

    const defaultColDef = useMemo(() => {
        return {
            flex: 1,
            sortable: true,
            filter: true,
            resizable: true,
        };
    }, []);

    useEffect(() => {
        const api = gridRef.current.api
        if (!api) {
            return
        }
        const es = new EventSource("/api/requests/stream");

        es.addEventListener('connected', (e) => {
            console.log("connected!")
        });

        es.addEventListener('r', (e) => {
            const rec = JSON.parse(e.data);
            console.log(rec)
            api.applyTransactionAsync({
                add: [rec],
                addIndex: 0,
            });
        });

        es.addEventListener('rs', (e) => {
            const rec = JSON.parse(e.data);
            // need to use synchronous since update might come in very soon after
            api.applyTransaction({
                add: [rec],
                addIndex: 0,
            });
        });

        es.addEventListener('rd', (e) => {
            const rec = JSON.parse(e.data);
            const existingRow = api.getRowNode(rec.i);
            if (!existingRow) {
                api.applyTransactionAsync({
                    add: [rec],
                    addIndex: 0,
                });
            } else {
                api.applyTransactionAsync({
                    update: [{ ...existingRow.data, d: rec.d, s: rec.s }],
                });
            }
        });

        es.onerror = () => {
            console.error("SSE connection error");
        };

        return () => es.close()
    }, [gridReady]);

    return (
        <div className="h-full w-full">
                <AgGridReact
                    ref={gridRef}
                    columnDefs={columnDefs}
                    defaultColDef={defaultColDef}
                    getRowId={(params) => String(params.data.i)}
                    onGridReady={() => setGridReady(true)}
                    theme={myTheme}
                />
        </div>
    );
}

// https://www.ag-grid.com/react-data-grid/styling-tutorial/
