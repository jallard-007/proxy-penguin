import {
    InfiniteRowModelModule,
    ModuleRegistry,
    ValidationModule,
    RowStyleModule,
    CellStyleModule,
    HighlightChangesModule,
    TextFilterModule,
    NumberFilterModule,
    DateFilterModule,
    BigIntFilterModule,
    CustomFilterModule,
    ClientSideRowModelModule,
    ClientSideRowModelApiModule,
    RowApiModule,
} from 'ag-grid-community';
import { AgGridProvider, AgGridReact } from 'ag-grid-react';

ModuleRegistry.registerModules([
    InfiniteRowModelModule,
    RowStyleModule,
    CellStyleModule,
    HighlightChangesModule,
    TextFilterModule,
    NumberFilterModule,
    DateFilterModule,
    BigIntFilterModule,
    CustomFilterModule,
    ClientSideRowModelModule,
    ClientSideRowModelApiModule,
    ValidationModule,
    RowApiModule
    // ...(process.env.NODE_ENV !== 'production' ? [ValidationModule] : []),
]);
