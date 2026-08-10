import React, { useEffect, useState } from 'react';
import {
  PageSection,
  Title,
  Card,
  CardBody,
  Toolbar,
  ToolbarContent,
  ToolbarItem,
  Button,
  EmptyState,
  EmptyStateIcon,
  EmptyStateBody,
  EmptyStateHeader,
  Spinner,
  Alert,
  Tabs,
  Tab,
  TabTitleText,
  Modal,
  ModalVariant,
  Form,
  FormGroup,
  TextInput,
  ActionGroup
} from '@patternfly/react-core';
import { Table, Thead, Tr, Th, Tbody, Td } from '@patternfly/react-table';
import { PlusCircleIcon, CubesIcon, TrashIcon, PencilAltIcon } from '@patternfly/react-icons';
import { fetchPolicy, savePolicy } from '../api';
import { PolicyData } from '../types';

const PolicyEditor: React.FunctionComponent = () => {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [policyData, setPolicyData] = useState<PolicyData>({ policies: [], groupingPolicies: [] });
  const [activeTabKey, setActiveTabKey] = useState<string | number>(0);
  
  // Modal State
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [modalType, setModalType] = useState<'policy' | 'grouping'>('policy');
  const [editIndex, setEditIndex] = useState<number | null>(null);
  
  // Form State
  const [subject, setSubject] = useState('');
  const [domain, setDomain] = useState('');
  const [object, setObject] = useState('');
  const [action, setAction] = useState('');
  const [user, setUser] = useState('');
  const [role, setRole] = useState('');

  useEffect(() => {
    loadPolicy();
  }, []);

  const loadPolicy = async () => {
    setLoading(true);
    try {
      const data = await fetchPolicy();
      setPolicyData(data);
      setError(null);
    } catch (err) {
      setError('Failed to load policy data');
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  const handleSave = async (newData: PolicyData) => {
    try {
      await savePolicy(newData);
      setPolicyData(newData);
      setIsModalOpen(false);
      resetForm();
    } catch (err) {
      setError('Failed to save policy data');
      console.error(err);
    }
  };

  const resetForm = () => {
    setSubject('');
    setDomain('');
    setObject('');
    setAction('');
    setUser('');
    setRole('');
    setEditIndex(null);
  };

  const openAddModal = (type: 'policy' | 'grouping') => {
    setModalType(type);
    resetForm();
    setIsModalOpen(true);
  };

  const openEditModal = (type: 'policy' | 'grouping', index: number) => {
    setModalType(type);
    setEditIndex(index);
    if (type === 'policy') {
      const p = policyData.policies[index];
      setSubject(p[0]);
      setDomain(p[1]);
      setObject(p[2]);
      setAction(p[3]);
    } else {
      const g = policyData.groupingPolicies[index];
      setUser(g[0]);
      setRole(g[1]);
    }
    setIsModalOpen(true);
  };

  const handleDelete = async (type: 'policy' | 'grouping', index: number) => {
    const newData = { ...policyData };
    if (type === 'policy') {
      newData.policies.splice(index, 1);
    } else {
      newData.groupingPolicies.splice(index, 1);
    }
    await handleSave(newData);
  };

  const handleModalSubmit = async () => {
    const newData = { ...policyData };
    if (modalType === 'policy') {
      const newRule = [subject, domain, object, action];
      if (editIndex !== null) {
        newData.policies[editIndex] = newRule;
      } else {
        newData.policies.push(newRule);
      }
    } else {
      const newRule = [user, role];
      if (editIndex !== null) {
        newData.groupingPolicies[editIndex] = newRule;
      } else {
        newData.groupingPolicies.push(newRule);
      }
    }
    await handleSave(newData);
  };

  const renderPolicyTable = () => (
    <>
      <Toolbar>
        <ToolbarContent>
          <ToolbarItem>
            <Button variant="primary" icon={<PlusCircleIcon />} onClick={() => openAddModal('policy')}>
              Add Policy Rule
            </Button>
          </ToolbarItem>
        </ToolbarContent>
      </Toolbar>
      <Table aria-label="Policy Table">
        <Thead>
          <Tr>
            <Th>Subject</Th>
            <Th>Domain (Datasource)</Th>
            <Th>Object (Resource)</Th>
            <Th>Action</Th>
            <Th>Actions</Th>
          </Tr>
        </Thead>
        <Tbody>
          {policyData.policies.map((row, index) => (
            <Tr key={index}>
              <Td dataLabel="Subject">{row[0]}</Td>
              <Td dataLabel="Domain">{row[1]}</Td>
              <Td dataLabel="Object">{row[2]}</Td>
              <Td dataLabel="Action">{row[3]}</Td>
              <Td dataLabel="Actions">
                <Button variant="plain" aria-label="Edit" onClick={() => openEditModal('policy', index)}>
                  <PencilAltIcon />
                </Button>
                <Button variant="plain" aria-label="Delete" onClick={() => handleDelete('policy', index)}>
                  <TrashIcon />
                </Button>
              </Td>
            </Tr>
          ))}
        </Tbody>
      </Table>
      {policyData.policies.length === 0 && (
        <EmptyState>
          <EmptyStateHeader titleText="No Policy Rules" headingLevel="h4" icon={<EmptyStateIcon icon={CubesIcon} />} />
          <EmptyStateBody>No access policies defined.</EmptyStateBody>
        </EmptyState>
      )}
    </>
  );

  const renderGroupingTable = () => (
    <>
      <Toolbar>
        <ToolbarContent>
          <ToolbarItem>
            <Button variant="primary" icon={<PlusCircleIcon />} onClick={() => openAddModal('grouping')}>
              Add Role Mapping
            </Button>
          </ToolbarItem>
        </ToolbarContent>
      </Toolbar>
      <Table aria-label="Grouping Policy Table">
        <Thead>
          <Tr>
            <Th>User</Th>
            <Th>Role</Th>
            <Th>Actions</Th>
          </Tr>
        </Thead>
        <Tbody>
          {policyData.groupingPolicies.map((row, index) => (
            <Tr key={index}>
              <Td dataLabel="User">{row[0]}</Td>
              <Td dataLabel="Role">{row[1]}</Td>
              <Td dataLabel="Actions">
                <Button variant="plain" aria-label="Edit" onClick={() => openEditModal('grouping', index)}>
                  <PencilAltIcon />
                </Button>
                <Button variant="plain" aria-label="Delete" onClick={() => handleDelete('grouping', index)}>
                  <TrashIcon />
                </Button>
              </Td>
            </Tr>
          ))}
        </Tbody>
      </Table>
      {policyData.groupingPolicies.length === 0 && (
        <EmptyState>
          <EmptyStateHeader titleText="No Role Mappings" headingLevel="h4" icon={<EmptyStateIcon icon={CubesIcon} />} />
          <EmptyStateBody>No role mappings defined.</EmptyStateBody>
        </EmptyState>
      )}
    </>
  );

  if (loading) {
    return (
      <PageSection>
        <Spinner />
      </PageSection>
    );
  }

  return (
    <>
      <PageSection>
        <Title headingLevel="h1" size="4xl">RBAC Policy Editor</Title>
      </PageSection>
      
      <PageSection>
        {error && <Alert variant="danger" title={error} actionClose={<Button variant="plain" onClick={() => setError(null)}>Close</Button>} />}
        
        <Card>
          <CardBody>
            <Tabs activeKey={activeTabKey} onSelect={(_: React.MouseEvent<HTMLElement, MouseEvent>, key: string | number) => setActiveTabKey(key)}>
              <Tab eventKey={0} title={<TabTitleText>Access Policies (p)</TabTitleText>}>
                {renderPolicyTable()}
              </Tab>
              <Tab eventKey={1} title={<TabTitleText>Role Mappings (g)</TabTitleText>}>
                {renderGroupingTable()}
              </Tab>
            </Tabs>
          </CardBody>
        </Card>
      </PageSection>

      <Modal
        variant={ModalVariant.medium}
        title={editIndex !== null ? `Edit ${modalType === 'policy' ? 'Policy' : 'Role Mapping'}` : `Add ${modalType === 'policy' ? 'Policy' : 'Role Mapping'}`}
        isOpen={isModalOpen}
        onClose={() => setIsModalOpen(false)}
      >
        <Form>
          {modalType === 'policy' ? (
            <>
              <FormGroup label="Subject" fieldId="subject" isRequired>
                <TextInput id="subject" value={subject} onChange={(_event: React.FormEvent, val: string) => setSubject(val)} />
              </FormGroup>
              <FormGroup label="Domain (Datasource)" fieldId="domain" isRequired>
                <TextInput id="domain" value={domain} onChange={(_event: React.FormEvent, val: string) => setDomain(val)} />
              </FormGroup>
              <FormGroup label="Object (Resource)" fieldId="object" isRequired>
                <TextInput id="object" value={object} onChange={(_event: React.FormEvent, val: string) => setObject(val)} />
              </FormGroup>
              <FormGroup label="Action" fieldId="action" isRequired>
                <TextInput id="action" value={action} onChange={(_event: React.FormEvent, val: string) => setAction(val)} />
              </FormGroup>
            </>
          ) : (
            <>
              <FormGroup label="User" fieldId="user" isRequired>
                <TextInput id="user" value={user} onChange={(_event: React.FormEvent, val: string) => setUser(val)} />
              </FormGroup>
              <FormGroup label="Role" fieldId="role" isRequired>
                <TextInput id="role" value={role} onChange={(_event: React.FormEvent, val: string) => setRole(val)} />
              </FormGroup>
            </>
          )}
          <ActionGroup>
            <Button variant="primary" onClick={handleModalSubmit}>Save</Button>
            <Button variant="link" onClick={() => setIsModalOpen(false)}>Cancel</Button>
          </ActionGroup>
        </Form>
      </Modal>
    </>
  );
};

export default PolicyEditor;
