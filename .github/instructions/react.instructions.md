---
applyTo: '**/*.tsx, **/*.jsx'
description: 'Rules for generating and refactoring React code'
---

# React Modular Architecture Rules

This document contains rules and guidelines for building modular React applications using established UI patterns and architectural principles. These rules are designed to create maintainable, scalable, and testable React applications.

## Core Principles

### 1. React is a View Library, Not a Framework
- **RULE**: Treat React as a library for building user interfaces only
- **RULE**: Do not mix business logic, data fetching, or state management directly in React components
- **RULE**: Consider your application as a JavaScript/TypeScript application that happens to use React for views

### 2. Separation of Concerns
- **RULE**: Always separate view logic from non-view logic
- **RULE**: Each module should have a single, well-defined responsibility
- **RULE**: Avoid mixing different levels of abstraction in the same file or component

## Component Design Rules

### 3. Component Responsibility
- **RULE**: Each component should focus on one thing only
- **RULE**: If a component is handling multiple concerns, split it into smaller components
- **RULE**: Components should be as pure as possible - given the same input, they produce the same output

### 4. Pure Components
- **RULE**: Prefer pure functional components over stateful components
- **RULE**: Extract state management into custom hooks
- **RULE**: Keep components focused on rendering UI only

### 5. Component Size
- **RULE**: If a component requires significant scrolling to read, it's too large
- **RULE**: Extract sub-components when you see repeated patterns or logical groupings
- **RULE**: Smaller components are more likely to be reusable

## State Management Rules

### 6. Custom Hooks for State
- **RULE**: Use custom hooks to encapsulate state management logic
- **RULE**: Hooks should return both state and functions to modify that state
- **RULE**: Name custom hooks with the `use` prefix (e.g., `usePaymentMethods`)

### 7. State Location
- **RULE**: Keep state as close to where it's used as possible
- **RULE**: Lift state up only when necessary for sharing between components
- **RULE**: Consider hooks as state machines that components can connect to

## Data Modeling Rules

### 8. Domain Objects
- **RULE**: Create domain classes/objects to encapsulate business logic
- **RULE**: Domain objects should not contain any UI-related information
- **RULE**: Use domain objects to avoid logic leakage in views

### 9. Data Conversion
- **RULE**: Handle data transformation in dedicated functions or classes
- **RULE**: Keep data conversion logic out of components
- **RULE**: Create a clear boundary between remote data formats and local domain objects

## Architecture Patterns

### 10. Layered Architecture
- **RULE**: Organize code into distinct layers: View, Model, and Data
- **RULE**: Each layer should only communicate with adjacent layers
- **RULE**: Dependencies should flow in one direction (typically View → Model → Data)

### 11. Anti-Corruption Layer
- **RULE**: Create gateway objects to encapsulate external system access
- **RULE**: Isolate API changes to a single location
- **RULE**: Convert external data formats to internal domain objects at the boundary

### 12. Polymorphism Over Conditionals
- **RULE**: Replace scattered if-else checks with polymorphic objects
- **RULE**: Use strategy pattern for country/locale-specific logic
- **RULE**: Avoid "shotgun surgery" by centralizing variant behavior

## Code Organization Rules

### 13. File Structure
```
src/
├── components/       # Pure UI components
├── hooks/           # Custom React hooks
├── models/          # Domain objects and business logic
├── services/        # API clients and external integrations
├── utils/           # Helper functions
└── types/           # TypeScript type definitions
```

### 14. Module Boundaries
- **RULE**: Each module should have clear inputs and outputs
- **RULE**: Avoid circular dependencies between modules
- **RULE**: Domain logic should never depend on UI logic

### 15. Naming Conventions
- **RULE**: Use descriptive names that indicate the module's responsibility
- **RULE**: Suffix view-related files with their type (e.g., `Payment.tsx` for components)
- **RULE**: Use consistent naming patterns across the codebase

## Testing and Reusability

### 16. Testability
- **RULE**: Pure components are easier to test - prefer them
- **RULE**: Domain objects should be fully testable without UI
- **RULE**: Hooks can be tested independently of components

### 17. Reusability Checklist
- **RULE**: Before creating a component, ask: "Could this be used elsewhere?"
- **RULE**: Extract common patterns into shared components
- **RULE**: Domain logic should be reusable across different UI frameworks

## Refactoring Guidelines

### 18. Progressive Enhancement
- **RULE**: Start simple and refactor as complexity grows
- **RULE**: Don't over-engineer from the beginning
- **RULE**: Apply patterns when they solve actual problems

### 19. Refactoring Triggers
- **RULE**: When adding a feature requires changes in multiple places → Extract shared logic
- **RULE**: When a component becomes hard to understand → Split it
- **RULE**: When you see repeated if-else for variants → Consider polymorphism

### 20. Code Smells to Avoid
- **RULE**: Avoid "shotgun surgery" - needing to change multiple files for one feature
- **RULE**: Avoid logic leakage - business rules scattered in views
- **RULE**: Avoid mixed concerns - components doing too many things

## Implementation Examples

### 21. Hook Pattern
```typescript
// GOOD: Extracted hook with clear responsibility
const useRoundUp = (amount: number, strategy: PaymentStrategy) => {
  const [agreeToDonate, setAgreeToDonate] = useState(false);
  
  const { total, tip } = useMemo(() => ({
    total: agreeToDonate ? strategy.getRoundUpAmount(amount) : amount,
    tip: strategy.getTip(amount),
  }), [agreeToDonate, amount, strategy]);
  
  return { total, tip, agreeToDonate, updateAgreeToDonate };
};
```

### 22. Pure Component Pattern
```typescript
// GOOD: Pure component focused on rendering
const PaymentMethods = ({ options }: { options: PaymentMethod[] }) => (
  <>
    {options.map((method) => (
      <label key={method.provider}>
        <input
          type="radio"
          name="payment"
          value={method.provider}
          defaultChecked={method.isDefaultMethod}
        />
        <span>{method.label}</span>
      </label>
    ))}
  </>
);
```

### 23. Domain Object Pattern
```typescript
// GOOD: Domain object encapsulating business logic
class PaymentMethod {
  constructor(private remotePaymentMethod: RemotePaymentMethod) {}
  
  get provider() {
    return this.remotePaymentMethod.name;
  }
  
  get label() {
    if (this.provider === 'cash') {
      return `Pay in ${this.provider}`;
    }
    return `Pay with ${this.provider}`;
  }
  
  get isDefaultMethod() {
    return this.provider === "cash";
  }
}
```

### 24. Strategy Pattern
```typescript
// GOOD: Strategy pattern for handling variants
class CountryPayment {
  constructor(
    private currencySign: string,
    private algorithm: RoundUpStrategy
  ) {}
  
  getRoundUpAmount(amount: number): number {
    return this.algorithm(amount);
  }
  
  getTip(amount: number): number {
    return calculateTipFor(this.getRoundUpAmount.bind(this))(amount);
  }
}
```

## Best Practices Summary

### 25. Development Workflow
1. **Start with a simple component**
2. **Extract hooks when state logic becomes complex**
3. **Create domain objects when business rules emerge**
4. **Split components when they handle multiple concerns**
5. **Introduce patterns when they solve real problems**

### 26. Decision Framework
- **Can this logic be used without React?** → Extract to domain object
- **Is this about managing state?** → Extract to custom hook
- **Is this about rendering UI?** → Keep in component
- **Is this about external systems?** → Create a service/gateway

### 27. Quality Checklist
- [ ] Components are focused and easy to understand
- [ ] Business logic is testable without UI
- [ ] Changes are localized to relevant modules
- [ ] Code can be reused in different contexts
- [ ] Dependencies flow in one direction

## Anti-Patterns to Avoid

### 28. Common Mistakes
- **AVOID**: Fetching data directly in components
- **AVOID**: Transforming data in render methods
- **AVOID**: Scattered conditional logic for variants
- **AVOID**: Mixing UI and business logic
- **AVOID**: Creating unnecessary temporary files or scripts

### 29. Code Review Red Flags
- Components over 200 lines
- Multiple `useEffect` hooks in one component
- Business logic in event handlers
- Repeated if-else checks across files
- Direct API calls in components

## Migration Strategy

### 30. Incremental Refactoring
1. **Identify the busiest components first**
2. **Extract custom hooks for state management**
3. **Create domain objects for business logic**
4. **Introduce service layers for external calls**
5. **Apply patterns gradually as needed**

Remember: The goal is to create a maintainable, scalable application where each part has a clear responsibility and can be understood, tested, and modified independently. 
