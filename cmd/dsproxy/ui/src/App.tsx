import React, { useState } from 'react';
import { 
  Page, 
  Masthead, 
  MastheadMain, 
  MastheadBrand, 
  MastheadContent, 
  PageSection, 
  Title, 
  EmptyState, 
  EmptyStateHeader, 
  EmptyStateIcon, 
  EmptyStateBody,
  Card,
  CardBody,
  Grid,
  GridItem,
  PageSidebar,
  PageSidebarBody,
  Nav,
  NavList,
  NavItem,
  PageToggleButton
} from '@patternfly/react-core';
import { CubesIcon, BarsIcon } from '@patternfly/react-icons';
import { Routes, Route, Link, useLocation } from 'react-router-dom';
import PolicyEditor from './routes/PolicyEditor';

const Dashboard: React.FunctionComponent = () => (
  <>
    <PageSection>
      <Title headingLevel="h1" size="4xl">Dashboard</Title>
    </PageSection>
    <PageSection>
      <Grid hasGutter>
        <GridItem span={12}>
          <Card>
            <CardBody>
              <EmptyState>
                <EmptyStateHeader 
                  titleText="Welcome to DSProxy" 
                  headingLevel="h4" 
                  icon={<EmptyStateIcon icon={CubesIcon} />} 
                />
                <EmptyStateBody>
                  This is the DSProxy UI - a datasource proxy enforcing multi-tenancy via JWT authentication,
                  Casbin authorization, and PromQL label injection.
                </EmptyStateBody>
              </EmptyState>
            </CardBody>
          </Card>
        </GridItem>
      </Grid>
    </PageSection>
  </>
);

const App: React.FunctionComponent = () => {
  const [isSidebarOpen, setIsSidebarOpen] = useState(true);
  const location = useLocation();

  const onSidebarToggle = () => {
    setIsSidebarOpen(!isSidebarOpen);
  };

  const header = (
    <Masthead>
      <MastheadMain>
        <PageToggleButton variant="plain" aria-label="Global navigation" isSidebarOpen={isSidebarOpen} onSidebarToggle={onSidebarToggle}>
          <BarsIcon />
        </PageToggleButton>
        <MastheadBrand>
          <Title headingLevel="h1" size="2xl">DSProxy</Title>
        </MastheadBrand>
      </MastheadMain>
      <MastheadContent>
        {/* Header content */}
      </MastheadContent>
    </Masthead>
  );

  const navigation = (
    <Nav>
      <NavList>
        <NavItem itemId={0} isActive={location.pathname === '/'}>
          <Link to="/">Dashboard</Link>
        </NavItem>
        <NavItem itemId={1} isActive={location.pathname === '/policy'}>
          <Link to="/policy">Policy Editor</Link>
        </NavItem>
      </NavList>
    </Nav>
  );

  const sidebar = (
    <PageSidebar isSidebarOpen={isSidebarOpen}>
      <PageSidebarBody>
        {navigation}
      </PageSidebarBody>
    </PageSidebar>
  );

  return (
    <Page header={header} sidebar={sidebar}>
      <Routes>
        <Route path="/" element={<Dashboard />} />
        <Route path="/policy" element={<PolicyEditor />} />
      </Routes>
    </Page>
  );
};

export default App;
