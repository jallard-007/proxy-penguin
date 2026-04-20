import { useState, useEffect, useMemo } from "react";

import { AgGridReact } from 'ag-grid-react';


// Custom Cell Renderer (Display flags based on cell value)
const CompanyLogoRenderer = ({ value }) => (
    <span style={{ display: "flex", height: "100%", width: "100%", alignItems: "center" }}>{value && <img alt={`${value} Flag`} src={`https://www.ag-grid.com/example-assets/space-company-logos/${value.toLowerCase()}.png`} style={{ display: "block", width: "25px", height: "auto", maxHeight: "50%", marginRight: "12px", filter: "brightness(1.1)" }} />}<p style={{ textOverflow: "ellipsis", overflow: "hidden", whiteSpace: "nowrap" }}>{value}</p></span>
);

const SuccessRenderer = ({ value }) => (
    <span
        style={{
            display: "flex",
            justifyContent: "center",
            height: "100%",
            alignItems: "center",
        }}
    >
        {
            <img
                alt={`${value}`}
                src={`https://www.ag-grid.com/example-assets/icons/${value ? "tick-in-circle" : "cross-in-circle"}.png`}
                style={{ width: "auto", height: "auto" }}
            />
        }
    </span>
);

// Create new GridExample component
const GridExample = () => {
    // Row Data: The data to be displayed.
    const [rowData, setRowData] = useState([]);
    const [colDefs, setColDefs] = useState([
        { field: "mission", filter: false },
        { field: "company", cellRenderer: CompanyLogoRenderer },
        { field: "location" },
        {
            field: "date",
            valueFormatter: params => {
                const d = new Date(params.value)
                return d.toLocaleDateString();
            }
        },
        {
            field: "price",
            valueFormatter: params => { return '$' + params.value.toLocaleString(); }
        },
        { field: "successful", cellRenderer: SuccessRenderer },
        { field: "rocket" }
    ]);

    // Fetch data & update rowData state
    useEffect(() => {
        fetch('https://www.ag-grid.com/example-assets/space-mission-data.json') // Fetch data from server
            .then(result => result.json()) // Convert to JSON
            .then(rowData => setRowData(rowData)); // Update state of `rowData`
    }, [])

    // Apply settings across all columns
    const defaultColDef = useMemo(() => ({
        filter: true // Enable filtering on all columns
    }))

    // https://www.ag-grid.com/react-data-grid/deep-dive/
    // https://www.ag-grid.com/react-data-grid/column-properties/
    // https://www.ag-grid.com/react-data-grid/grid-options/
    // https://www.ag-grid.com/react-data-grid/value-formatters/
    // https://www.ag-grid.com/react-data-grid/component-cell-renderer/

    // Container: Defines the grid's theme & dimensions.
    return (
        <div style={{ width: "100%", height: "100%" }}>
            <AgGridReact
                rowData={rowData}
                columnDefs={colDefs}
                rowModelType={"infinite"}
                defaultColDef={defaultColDef}
            />
        </div>
    );
};